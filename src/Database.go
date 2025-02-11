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

const (
	FunctionNameMaxLen     = 50
	FunctionLanguageMaxLen = 50
	FunctionCodeMaxLen     = 10 * 1000
	ContainerIdMaxLen      = 100 // max docker container name is 63
)

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
		return nil, fmt.Errorf("Could not connect to database")
	}

	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS functions (
		id INT AUTO_INCREMENT PRIMARY KEY,
		function_name VARCHAR(%d) NOT NULL,
		function_language VARCHAR(%d) NOT NULL,
		function_code VARCHAR(%d) NOT NULL,
		container_id VARCHAR(%d)
	)`, FunctionNameMaxLen, FunctionLanguageMaxLen, FunctionCodeMaxLen, ContainerIdMaxLen)

	_, err = db.Exec(query)
	if err != nil {
		log.Printf("database error Checking functions table: %v", err)
		return nil, fmt.Errorf("Could not connect to database")
	}

	return &FunctionDB{db: db}, nil
}

func (fDB *FunctionDB) InsertFunction(functionName, functionLanguage, functionCode string) (uid string, err error) {
	if functionName == "" || functionLanguage == "" || functionCode == "" {
		return "", fmt.Errorf("database insertion requires non empty values for functionName, language, and code")
	} else if len(functionName) > FunctionNameMaxLen || len(functionLanguage) > FunctionLanguageMaxLen || len(functionCode) > FunctionCodeMaxLen {
		return "", fmt.Errorf("database insertion requires function name, code, or language is too long")
	}

	tx, err := fDB.db.Begin()
	if err != nil {
		log.Printf("database failed to start transaction: %v", err)
		return "", fmt.Errorf("database internal error")
	}
	defer tx.Rollback()

	res, err := fDB.db.Exec(
		"INSERT INTO functions (function_name, function_language, function_code) VALUES (?, ?, ?)",
		functionName, functionLanguage, functionCode,
	)
	if err != nil {
		log.Printf("database failed to insert: %v", err)
		return "", fmt.Errorf("database internal error")
	}

	lID, err := res.LastInsertId()
	if err != nil {
		log.Printf("database failed to get last inserted row Id: %v", err)
		return "", fmt.Errorf("database internal error")
	}

	var fun FunctionEntry
	row := tx.QueryRow("SELECT id, function_name, function_language, function_code, container_id FROM functions WHERE id = ?", lID)
	err = row.Scan(&fun.ID, &fun.FunctionName, &fun.FunctionLanguage, &fun.FunctionCode, &fun.ContainerID)
	if err != nil {
		log.Printf("database failed to get the new row: %v", err)
		return "", fmt.Errorf("database internal error")
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("database failed to commit the changes made: %v", err)
		return "", fmt.Errorf("database internal error")
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
