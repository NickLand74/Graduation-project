package main

import (
	"database/sql"
	"encoding/json"
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

type Task struct {
	Date    string `json:"date"`
	Title   string `json:"title"`
	Comment string `json:"comment,omitempty"`
	Repeat  string `json:"repeat,omitempty"`
}

type TaskResponse struct {
	ID    int    `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}

func main() {
	port := os.Getenv("TODO_PORT")
	if port == "" {
		port = "7540"
	}

	webDir := "./web"
	http.Handle("/", http.FileServer(http.Dir(webDir)))

	dbFile := filepath.Join(".", "scheduler.db")
	fmt.Println("Путь к базе данных:", dbFile)

	_, err := os.Stat(dbFile)
	if err != nil {
		log.Println("Файл базы данных не найден:", err)
	}

	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := createTasksTable(db); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/api/task", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")

		switch r.Method {
		case http.MethodPost:
			handleAddTask(w, r, db)
		case http.MethodGet:
			handleGetTask(w, r, db)
		case http.MethodDelete:
			handleDeleteTask(w, r, db)
		default:
			http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		}
	})

	fmt.Printf("Сервер запущен на http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Println("Ошибка запуска сервера:", err)
	}
}

func createTasksTable(db *sql.DB) error {
	createTableSQL := `CREATE TABLE IF NOT EXISTS tasks (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        date TEXT NOT NULL,
        comment TEXT,
        repeat TEXT
    );`
	_, err := db.Exec(createTableSQL)
	return err
}

func handleAddTask(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var task Task
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		http.Error(w, "Ошибка десериализации JSON", http.StatusBadRequest)
		return
	}

	if task.Title == "" {
		json.NewEncoder(w).Encode(TaskResponse{Error: "Не указан заголовок задачи"})
		return
	}

	if task.Date == "" {
		task.Date = time.Now().Format("20060102")
	}
	parsedDate, err := time.Parse("20060102", task.Date)
	if err != nil {
		json.NewEncoder(w).Encode(TaskResponse{Error: "Неверный формат даты"})
		return
	}

	today := time.Now()
	if parsedDate.Before(today) {
		if task.Repeat == "" {
			task.Date = today.Format("20060102")
		} else {
			nextDate, err := NextDate(today, task.Date, task.Repeat)
			if err != nil {
				json.NewEncoder(w).Encode(TaskResponse{Error: "Неверное правило повторения"})
				return
			}
			task.Date = nextDate
		}
	}

	res, err := db.Exec("INSERT INTO tasks (name, date, comment, repeat) VALUES (?, ?, ?, ?)", task.Title, task.Date, task.Comment, task.Repeat)
	if err != nil {
		http.Error(w, "Ошибка добавления задачи в базу данных", http.StatusInternalServerError)
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		http.Error(w, "Ошибка получения ID задачи", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(TaskResponse{ID: int(id)})
}

func handleGetTask(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	taskId := r.URL.Query().Get("id")
	if taskId == "" {
		http.Error(w, "Не указан ID задачи", http.StatusBadRequest)
		return
	}

	var task Task
	err := db.QueryRow("SELECT name, date, comment, repeat FROM tasks WHERE id = ?", taskId).Scan(&task.Title, &task.Date, &task.Comment, &task.Repeat)
	if err != nil {
		http.Error(w, "Задача не найдена", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(task)
}

func handleDeleteTask(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	taskId := r.URL.Query().Get("id")
	if taskId == "" {
		http.Error(w, "Не указан ID задачи", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("DELETE FROM tasks WHERE id = ?", taskId)
	if err != nil {
		http.Error(w, "Ошибка удаления задачи", http.StatusInternalServerError)
		return
	}
	idInt, err := strconv.Atoi(taskId)
	if err != nil {
		http.Error(w, "Некорректный формат ID задачи", http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(TaskResponse{ID: idInt})

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
