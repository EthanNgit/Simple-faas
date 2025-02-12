package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"regexp"
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

	// retry connection for 30 seconds
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

	// TODO: perhaps extract this out to another container service, but for now here
	// create the tables needed
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
		return nil, fmt.Errorf("could not connect to database")
	}

	query = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS running_containers (
		container_id VARCHAR(%d) PRIMARY KEY,
		function_id INT,
		last_used DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (function_id) REFERENCES functions(id)
	)`, ContainerIdMaxLen)

	_, err = db.Exec(query)
	if err != nil {
		log.Printf("database error Checking running_containers table: %v", err)
		return nil, fmt.Errorf("could not connect to database")
	}

	return &FunctionDB{db: db}, nil
}

func (fDB *FunctionDB) InsertFunction(functionName, functionLanguage, functionCode string) (uid string, err error) {
	// check basic validation on input
	if functionName == "" || functionLanguage == "" || functionCode == "" {
		return "", fmt.Errorf("database insertion requires non empty values for functionName, language, and code")
	} else if len(functionName) > FunctionNameMaxLen || len(functionLanguage) > FunctionLanguageMaxLen || len(functionCode) > FunctionCodeMaxLen {
		return "", fmt.Errorf("database insertion requires function name, code, or language is too long")
	}

	// ensure atomicity
	tx, err := fDB.db.Begin()
	if err != nil {
		log.Printf("database failed to start transaction: %v", err)
		return "", fmt.Errorf("database internal error")
	}
	defer tx.Rollback()

	// insert the function
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

	// get the function to make the uid
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

	// update the container id
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

	// get the function
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

	// TODO: perhaps add a way to delete functions via api
	// delete the function
	_, err := fDB.db.Exec("DELETE FROM functions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("database error deleting function %s(%s): %v", uid, id, err)
	}

	return nil
}

func (fDB *FunctionDB) generateFunctionID(entry FunctionEntry) (id string) {
	//TODO: if a number gets over 64 characters
	name := strings.ToLower(strings.ReplaceAll(entry.FunctionName, "_", "-")) // only allow '-'

	re := regexp.MustCompile(`[^a-z0-9\-]`) // clear any non alphanumeric characters + '-'
	name = re.ReplaceAllString(name, "")

	// suffix is mandatory "-n", where n is the id (32Int, where it is not possible to be 63 in len)
	suffix := "-" + strconv.Itoa(entry.ID)

	// sacrifice the name in exchange for the id of the container
	maxNameLength := 63 - len(suffix)
	if maxNameLength < 1 {
		maxNameLength = 0
	}
	if len(name) > maxNameLength {
		name = name[:maxNameLength]
	}

	return name + suffix
}

func (fDB *FunctionDB) solveFunctionID(uid string) string {
	// since the name is "name-n" where n is the id, we just need to get it
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

func (fDB *FunctionDB) UpdateLastUsedTime(cid, uid string, create bool) error {
	// TODO: could make the api of this function easier, but not really needed
	id := fDB.solveFunctionID(uid)

	// insert the log of it running or update it if it exists
	if create {
		_, err := fDB.db.Exec("INSERT INTO running_containers (container_id, function_id) VALUES (?, ?)", cid, id)
		if err != nil {
			log.Printf("failed to insert last used time for container %s: %v", id, err)
			return fmt.Errorf("database failed to update last used time")
		}
	} else {
		_, err := fDB.db.Exec("UPDATE running_containers SET last_used = NOW() WHERE function_id = ?", id)
		if err != nil {
			log.Printf("failed to update last used time for container %s: %v", id, err)
			return fmt.Errorf("database failed to update last used time")
		}
	}

	return nil
}
