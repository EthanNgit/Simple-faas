package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/gin-gonic/gin"
)

type CreateRequest struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type InvokeRequest struct {
	Name   string                 `json:"name"`
	Params map[string]interface{} `json:"params"`
}

type FuncDef struct {
	FuncCode string
	FuncCntr string
}

type Engine struct {
	cntrCli   *client.Client
	functions map[string]FuncDef
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
		functions: make(map[string]FuncDef),
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
	return nil
}

func (e *Engine) InvokeFunction(name string, params map[string]interface{}) (interface{}, error) {
	return nil, nil
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

func main() {
	engine, err := NewEngine()
	if err != nil {
		panic(err)
	}

	fmt.Printf("[FAAS-debug] Engine id %s\n", engine.engId)

	// Setup api routes
	router := gin.Default()
	router.POST("/api/functions/v1/create", engine.handleCreate)
	router.POST("/api/functions/v1/invoke", engine.handleInvoke)

	// Run the api loop
	fmt.Println("[FAAS-debug] Starting server")
	router.Run(":8080")
}
