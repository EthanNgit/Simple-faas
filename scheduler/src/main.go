package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	db, err := ConnectDb()
	if err != nil {
		log.Fatal(err)
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("failed to connect to container client %v", err)
		log.Fatal("failed to start engine")
	}

	interval, _ := time.ParseDuration(os.Getenv("CLEANUP_INTERVAL"))
	if interval == 0 {
		interval = 5 * time.Minute
	}
	log.Printf("Interval is set to poll every %s minutes", interval.String())

	ticker := time.NewTicker(interval)
	for range ticker.C {
		CleanupContainers(db, cli)
	}
}

func ConnectDb() (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"),
	)

	var db *sql.DB
	var err error

	// Retry connection for 30 seconds
	for i := 0; i < 10; i++ {
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			log.Printf("Error opening database: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		err = db.Ping()
		if err == nil {
			break
		}
		log.Println("Database not ready, retrying...")
		time.Sleep(3 * time.Second)
	}

	if err != nil {
		log.Printf("database error starting database: %v", err)
		return nil, fmt.Errorf("could not connect to database")
	}

	return db, nil
}

func CleanupContainers(db *sql.DB, cli *client.Client) {
	ctx := context.Background()
	deleteAfter, _ := time.ParseDuration(os.Getenv("DELETE_AFTER"))

	rows, err := db.Query(`
		SELECT container_id 
		FROM running_containers 
		WHERE last_used < NOW() - INTERVAL ? MINUTE
	`, int(deleteAfter.Minutes()))
	if err != nil {
		log.Printf("failed to fetch running containers: %v", err)
		return
	}

	for rows.Next() {
		var cID string
		if err := rows.Scan(&cID); err != nil {
			continue
		}

		if err := cli.ContainerStop(ctx, cID, container.StopOptions{}); err != nil {
			log.Printf("failed to stop running container %s: %v", cID, err)
			return
		}

		_, err := db.Exec(`
			DELETE FROM running_containers 
			WHERE container_id = ?
		`, cID)
		if err != nil {
			log.Printf("failed to delete running container entry for %s: %v", cID, err)
			return
		}
	}
}
