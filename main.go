package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello, world!")
}

func main() {
	port := os.Getenv("TODO_PORT")
	if port == "" {
		port = "7540"
	}

	http.HandleFunc("/", Handler)

	// Создаем путь к базе данных в корне проекта
	dbFile := filepath.Join(".", "scheduler.db")
	fmt.Println("Путь к базе данных:", dbFile)

	_, err := os.Stat(dbFile)

	var install bool
	if err != nil {
		install = true
	}

	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var tableExists bool
	err = db.QueryRow("SELECT count(*) > 0 FROM sqlite_master WHERE type='table' AND name='tasks';").Scan(&tableExists)
	if err != nil {
		log.Fatal(err)
	}

	if !tableExists {
		fmt.Println("Таблица 'tasks' не существует, создаем ее...")
		createTableSQL := `CREATE TABLE IF NOT EXISTS tasks (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        completed BOOLEAN NOT NULL DEFAULT FALSE
      );`
		if _, err := db.Exec(createTableSQL); err != nil {
			log.Fatalf("Ошибка при создании таблицы: %v", err)
		}
		fmt.Println("База данных создана и таблицы добавлены.")
	} else {
		fmt.Println("База данных и таблица уже существуют.")
	}

	if install {
		fmt.Println("База данных не существовала, она была создана.")
	}

	fmt.Printf("Сервер запущен на :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Println("Ошибка запуска сервера:", err)
	}
}

func NextDate(now time.Time, date string, repeat string) (string, error) {
	if repeat == "" {
		return "", fmt.Errorf("пустое правило повторения")
	}
	parsedDate, err := time.Parse("20060102", date)
	if err != nil {
		return "", fmt.Errorf("некорректная дата: %s", date)
	}

	nextDate := parsedDate

	switch {
	case strings.HasPrefix(repeat, "d "):
		daysStr := strings.TrimSpace(strings.TrimPrefix(repeat, "d "))
		days, err := strconv.Atoi(daysStr)
		if err != nil || days <= 0 || days > 400 {
			return "", fmt.Errorf("недопустимый интервал дней: %s", daysStr)
		}
		nextDate = nextDate.AddDate(0, 0, days)

	case repeat == "y":
		if parsedDate.Month() == 2 && parsedDate.Day() == 29 {
			// Если дата - 29 февраля, добавляем 1 год
			nextDate = parsedDate.AddDate(1, 0, 0)
			if !isLeapYear(nextDate.Year()) {
				nextDate = time.Date(nextDate.Year(), 2, 28, 0, 0, 0, 0, nextDate.Location())
			}
		} else {
			nextDate = parsedDate.AddDate(1, 0, 0)
			if parsedDate.Day() == 31 {
				// Переход на конец месяца
				nextDate = time.Date(nextDate.Year(), nextDate.Month()+1, 0, 0, 0, 0, 0, nextDate.Location())
			}
		}

	default:
		return "", fmt.Errorf("неподдерживаемый формат: %s", repeat)
	}

	if !nextDate.After(now) {
		return "", fmt.Errorf("следующая дата должна быть больше текущей")
	}

	return nextDate.Format("20060102"), nil
}

func isLeapYear(year int) bool {
	return (year%4 == 0 && year%100 != 0) || (year%400 == 0)
}
