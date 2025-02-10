package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type FunctionDB struct {
	db *sql.DB
}

type FunctionEntry struct {
	ID               int
	FunctionName     string
	FunctionCode     string
	FunctionLanguage string
	ContainerID      sql.NullString
}

func ConnectDb() (*FunctionDB, error) {
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
			log.Println("Error opening database:", err)
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
		return nil, fmt.Errorf("database error starting database: %v", err)
	}

	query := `CREATE TABLE IF NOT EXISTS functions (
		id INT AUTO_INCREMENT PRIMARY KEY,
		function_name VARCHAR(50) NOT NULL,
		function_language VARCHAR(50) NOT NULL,
		function_code VARCHAR(10000) NOT NULL,
		container_id VARCHAR(100)
	)`

	_, err = db.Exec(query)
	if err != nil {
		return nil, fmt.Errorf("database error Checking functions table: %v", err)
	}

	return &FunctionDB{db: db}, nil
}

func (fDB *FunctionDB) InsertFunction(functionName, functionLanguage, functionCode string) (uid string, err error) {
	tx, err := fDB.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	res, err := fDB.db.Exec(
		"INSERT INTO functions (function_name, function_language, function_code) VALUES (?, ?, ?)",
		functionName, functionLanguage, functionCode,
	)
	if err != nil {
		return "", fmt.Errorf("database failed to insert: %v", err)
	}

	lID, err := res.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("database failed to get last inserted row Id: %v", err)
	}

	var fun FunctionEntry
	row := tx.QueryRow("SELECT id, function_name, function_language, function_code, container_id FROM functions WHERE id = ?", lID)
	err = row.Scan(&fun.ID, &fun.FunctionName, &fun.FunctionLanguage, &fun.FunctionCode, &fun.ContainerID)
	if err != nil {
		return "", fmt.Errorf("database failed to get the new row: %v", err)
	}

	err = tx.Commit()
	if err != nil {
		return "", fmt.Errorf("database failed to commit the changes made: %v", err)
	}

	return fDB.generateFunctionID(fun), nil
}

func (fDB *FunctionDB) UpdateCIDToFunction(uid, containerId string) error {
	id := fDB.solveFunctionID(uid)

	_, err := fDB.db.Exec(
		"UPDATE functions SET container_id = ? WHERE id = ?",
		containerId, id,
	)
	if err != nil {
		return fmt.Errorf("database failed to update %s(%s): %v", uid, id, err)
	}

	return nil
}

func (fDB *FunctionDB) GetFunction(uid string) (function FunctionEntry, err error) {
	id := fDB.solveFunctionID(uid)

	var fun FunctionEntry
	row := fDB.db.QueryRow("SELECT id, function_name, function_language, function_code, container_id FROM functions WHERE id = ?", id)
	err = row.Scan(&fun.ID, &fun.FunctionName, &fun.FunctionLanguage, &fun.FunctionCode, &fun.ContainerID)
	if err != nil {
		return FunctionEntry{}, fmt.Errorf("database error getting function %s(%s): %v", uid, id, err)
	}

	return fun, nil
}

func (fDB *FunctionDB) DeleteFunction(uid string) error {
	id := fDB.solveFunctionID(uid)

	_, err := fDB.db.Exec("DELETE FROM functions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("database error deleting function %s(%s): %v", uid, id, err)
	}

	return nil
}

func (fDB *FunctionDB) generateFunctionID(entry FunctionEntry) (id string) {
	// TODO: make more advanced <64, cleansing etc
	return strings.ToLower(strings.ReplaceAll(entry.FunctionName, "_", "-")) + "-" + strconv.Itoa(entry.ID)
}

func (fDB *FunctionDB) solveFunctionID(uid string) string {
	index := strings.LastIndex(uid, "-")

	if index != -1 {
		substring := uid[index+1:]
		return substring
	} else {
		return ""
	}
}

func (fDB *FunctionDB) Close() error {
	return fDB.db.Close()
}
