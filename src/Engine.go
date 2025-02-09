package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/gin-gonic/gin"
)

type Engine struct {
	cntrCli   *client.Client
	functions map[string]FuncInfo
	netName   string
	engId     string
	mu        sync.Mutex
}

func NewEngine() (*Engine, error) {
	// connect to docker daemon
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	// get a id for this container, random should be good enough
	engId := os.Getenv("ENGINE_ID")
	if engId == "" {
		engId = generateEngineId()
	}

	e := &Engine{
		cntrCli:   cli,
		functions: make(map[string]FuncInfo),
		netName:   "faas-net",
		engId:     engId,
	}

	// since we can support multiple engines, make sure to verify the network
	ctx := context.Background()
	_, err = cli.NetworkInspect(ctx, e.netName, types.NetworkInspectOptions{})
	if err != nil {
		// try to create an network if it doesnt exist
		_, err = cli.NetworkCreate(ctx, e.netName, types.NetworkCreate{
			Driver: "bridge",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create network %v", err)
		}
	}

	return e, nil
}

func generateEngineId() string {
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("engine-%d", rand.Intn(100000))
}

func (e *Engine) CreateFunction(name, code string) error {
	// for concurrency issues
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.functions[name]; exists {
		return fmt.Errorf("function %s already exists", name)
	}

	e.functions[name] = FuncInfo{FuncCode: code}
	return nil
}

func (e *Engine) InvokeFunction(name string, params map[string]interface{}) (interface{}, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	info, exists := e.functions[name]
	if !exists {
		return nil, fmt.Errorf("function %s not found", name)
	}

	containerName := fmt.Sprintf("faas-%s-%s", e.engId, name)
	ctx := context.Background()

	containers, err := e.cntrCli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("name", containerName),
			filters.Arg("label", fmt.Sprintf("faas_engine_id=%s", e.engId)),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers %v", err)
	}

	if len(containers) == 0 {
		// create the container if it doesnt exist
		resp, err := e.cntrCli.ContainerCreate(ctx,
			&container.Config{
				Image: "faas-base-image",
				Env:   []string{fmt.Sprintf("FUNCTION_CODE=%s", info.FuncCode)},
				Labels: map[string]string{
					"faas_engine_id": e.engId,
					"faas_function":  name,
				},
			},
			&container.HostConfig{
				NetworkMode: container.NetworkMode(e.netName),
			},
			&network.NetworkingConfig{
				EndpointsConfig: map[string]*network.EndpointSettings{
					e.netName: {},
				},
			},
			nil,
			containerName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create container %v", err)
		}

		if err := e.cntrCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
			return nil, fmt.Errorf("failed to start container: %v", err)
		}

		info.FuncCntr = containerName
		e.functions[name] = info

		// Wait for the container to become ready
		if err := e.waitForContainerReady(containerName, 15*time.Second); err != nil {
			// Clean up the failed container
			_ = e.cntrCli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			return nil, fmt.Errorf("function initialization failed: %v", err)
		}
	}

	// now invoke the function that the container exists
	url := fmt.Sprintf("http://%s:5000/invoke", containerName)
	res, err := e.invokeHttpRequest(url, params)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke function: %v", err)
	}

	return res, nil
}

func (e *Engine) waitForContainerReady(containerName string, timeout time.Duration) error {
	client := http.DefaultClient
	url := fmt.Sprintf("http://%s:5000/health", containerName)
	startTime := time.Now()

	// Exponential backoff with jitter
	backoff := 500 * time.Millisecond
	maxBackoff := 5 * time.Second

	for {
		// Check timeout
		if time.Since(startTime) > timeout {
			return fmt.Errorf("container %s did not become ready within %v", containerName, timeout)
		}

		// Send health check request
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			return nil // Container is ready
		}

		// If the container crashed, abort immediately
		ctx := context.Background()
		containers, _ := e.cntrCli.ContainerList(ctx, container.ListOptions{
			Filters: filters.NewArgs(filters.Arg("name", containerName)),
		})
		if len(containers) == 0 {
			return fmt.Errorf("container %s failed to start", containerName)
		}

		// Wait with exponential backoff
		time.Sleep(backoff)
		backoff = time.Duration(float64(backoff) * 1.5)
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (e *Engine) invokeHttpRequest(url string, params map[string]interface{}) (interface{}, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	jsonParams, err := json.Marshal(map[string]interface{}{"params": params})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params %v", err)
	}

	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonParams))
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var result struct {
		Result interface{} `json:"result"`
		Error  string      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response %v", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("function error: %s", result.Error)
	}

	return result.Result, nil
}

func (e *Engine) handleCreate(c *gin.Context) {
	var req CreateRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := e.CreateFunction(req.Name, req.Code); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusCreated)
}

func (e *Engine) handleInvoke(c *gin.Context) {
	var req InvokeRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := e.InvokeFunction(req.Name, req.Params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": result})
}
