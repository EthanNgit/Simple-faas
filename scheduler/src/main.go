package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

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
	log.Printf("called to cleanup and has c and d %s %s\n", db, cli)
}
