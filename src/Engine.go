package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	cntrCli         *client.Client
	cachedFunctions map[string]FunctionEntry
	netName         string
	engId           string
	mu              sync.Mutex
	db              *FunctionDB
}

func NewEngine() (*Engine, error) {
	// connect to docker daemon
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("failed to connect to container client %v", err)
		return nil, fmt.Errorf("failed to start engine")
	}

	// get a id for this container, random should be good enough
	engId := os.Getenv("ENGINE_ID")
	if engId == "" {
		engId = generateEngineId()
	}

	db, err := ConnectDb()
	if err != nil {
		log.Printf("failed to connect to database %v", err)
		return nil, fmt.Errorf("failed to start engine")
	}

	e := &Engine{
		cntrCli:         cli,
		cachedFunctions: make(map[string]FunctionEntry),
		netName:         "faas-net",
		engId:           engId,
		db:              db,
	}

	// since we can support multiple engines, make sure to verify the network
	ctx := context.Background()
	_, err = cli.NetworkInspect(ctx, e.netName, types.NetworkInspectOptions{})
	if err != nil {
		// try to create an network if it doesn't exist
		_, err = cli.NetworkCreate(ctx, e.netName, types.NetworkCreate{
			Driver: "bridge",
		})
		if err != nil {
			log.Printf("failed to create network %v", err)
			return nil, fmt.Errorf("failed to start engine")
		}
	}

	return e, nil
}

func generateEngineId() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("engine-%d", r.Intn(100000))
}

func (e *Engine) CreateFunction(name, code string) (string, error) {
	// for concurrency issues
	e.mu.Lock()
	defer e.mu.Unlock()

	// insert the function
	uid, err := e.db.InsertFunction(name, "go", code)
	if err != nil {
		log.Printf("engine error inserting %s: %v", name, err)
		return "", fmt.Errorf("failed to create function")
	}

	// create the container
	ctx := context.Background()
	resp, err := e.cntrCli.ContainerCreate(ctx,
		&container.Config{
			Image: "faas-base-image",
			Env:   []string{fmt.Sprintf("FUNCTION_CODE=%s", code)},
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
		uid,
	)
	if err != nil {
		// backtrack insertion
		err2 := e.db.DeleteFunction(uid)
		if err2 != nil {
			log.Printf("engine error deleting %s: %v", name, err2)
			return "", fmt.Errorf("failed to create function")
		}

		log.Printf("engine error deleting %s: %v", name, err)
		return "", fmt.Errorf("failed to create function")
	}

	// update the cid in db
	err = e.db.UpdateCIDToFunction(uid, resp.ID)
	if err != nil {
		log.Printf("engine updating container id for %s(%s): %v", uid, name, err)
		return "", fmt.Errorf("failed to create function")
	}

	fun, err := e.db.GetFunction(uid)
	if err != nil {
		log.Printf("engine getting information about %s(%s): %v", uid, name, err)
		return "", fmt.Errorf("failed to create function")
	}

	e.cachedFunctions[uid] = fun
	return uid, nil
}

func (e *Engine) InvokeFunction(uid string, params map[string]interface{}) (interface{}, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	fun, exists := e.cachedFunctions[uid]
	if !exists {
		var err error
		fun, err = e.db.GetFunction(uid)
		if err != nil {
			log.Printf("engine could not find function in cache or storage: %v", err)
			return nil, fmt.Errorf("failed to invoke function")
		}
	}

	ctx := context.Background()
	inspect, err := e.cntrCli.ContainerInspect(ctx, fun.ContainerID.String)
	if err != nil {
		log.Printf("engine could not find container for %s: %v", fun.ContainerID.String, err)
		return nil, fmt.Errorf("failed to invoke function")
	}

	if !inspect.State.Running {
		if err := e.cntrCli.ContainerStart(
			ctx,
			fun.ContainerID.String,
			container.StartOptions{},
		); err != nil {
			log.Printf("engine could not start container for %s: %v", fun.ContainerID.String, err)
			return nil, fmt.Errorf("failed to invoke function")
		}

		if err := e.waitForContainerReady(
			uid,
			10*time.Second,
		); err != nil {
			log.Printf("engine could not wait container for %s: %v", fun.ContainerID.String, err)
			return nil, fmt.Errorf("failed to invoke function")
		}
	}

	return e.invokeHttpRequest(
		fmt.Sprintf("http://%s:5000/invoke", uid),
		params,
	)
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
			log.Printf("container %s did not become ready within %v", containerName, timeout)
			return fmt.Errorf("failed to invoke function")
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
			log.Printf("container %s failed to start", containerName)
			return fmt.Errorf("failed to invoke function")
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
		log.Printf("failed to marshal params %v", err)
		return nil, fmt.Errorf("failed to invoke function")
	}

	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonParams))
	if err != nil {
		log.Printf("failed to make HTTP request %v", err)
		return nil, fmt.Errorf("failed to invoke function")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("unexpected status code %d", resp.StatusCode)
		return nil, fmt.Errorf("failed to invoke function")
	}

	var result struct {
		Result interface{} `json:"result"`
		Error  string      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("failed to decode response %v", err)
		return nil, fmt.Errorf("failed to invoke function")
	}
	if result.Error != "" {
		log.Printf("function error: %s", result.Error)
		return nil, fmt.Errorf("failed to invoke function")
	}

	return result.Result, nil
}

func (e *Engine) handleCreate(c *gin.Context) {
	var req CreateRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := e.CreateFunction(req.Name, req.Code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"result": result})
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

func (e *Engine) Close() error {
	err := e.cntrCli.Close()
	if err != nil {
		return err
	}

	return e.db.Close()
}
