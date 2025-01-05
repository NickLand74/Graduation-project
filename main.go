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

// Дополнительная структура для вывода задачи списком (все поля - строки)
type TaskForJSON struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Title   string `json:"title"`
	Comment string `json:"comment"`
	Repeat  string `json:"repeat"`
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

	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := createSchedulerTable(db); err != nil {
		log.Fatal("Ошибка создания таблицы scheduler:", err)
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
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(TaskResponse{Error: "Метод не поддерживается"})
		}
	})

	// Новый маршрут для списка задач
	http.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]string{"error": "Метод не поддерживается"})
			return
		}
		handleGetTasks(w, r, db)
	})

	fmt.Printf("Сервер запущен на http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Println("Ошибка запуска сервера:", err)
	}
}

func createSchedulerTable(db *sql.DB) error {
	createTableSQL := `CREATE TABLE IF NOT EXISTS scheduler (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        title TEXT NOT NULL,
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
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка десериализации JSON"})
		return
	}

	if task.Title == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Не указан заголовок задачи"})
		return
	}

	if task.Date == "" {
		task.Date = time.Now().Format("20060102")
	}

	parsedDate, err := time.Parse("20060102", task.Date)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Неверный формат даты"})
		return
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	if parsedDate.Before(today) {
		if task.Repeat == "" {
			task.Date = today.Format("20060102")
		} else {
			nextDate, err := NextDate(today, task.Date, task.Repeat)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(TaskResponse{Error: "Неверное правило повторения"})
				return
			}
			task.Date = nextDate
		}
	}

	res, err := db.Exec("INSERT INTO scheduler (title, date, comment, repeat) VALUES (?, ?, ?, ?)",
		task.Title, task.Date, task.Comment, task.Repeat)
	if err != nil {
		log.Printf("Ошибка добавления задачи: Title=%s, Date=%s, Comment=%s, Repeat=%s, error: %v",
			task.Title, task.Date, task.Comment, task.Repeat, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка добавления задачи в базу данных"})
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка получения ID задачи"})
		return
	}

	json.NewEncoder(w).Encode(TaskResponse{ID: int(id)})
	log.Printf("Задача добавлена: ID=%d, Title=%s", id, task.Title)
}

func handleGetTask(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	taskId := r.URL.Query().Get("id")
	if taskId == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Не указан ID задачи"})
		return
	}

	var task Task
	err := db.QueryRow("SELECT title, date, comment, repeat FROM scheduler WHERE id = ?", taskId).
		Scan(&task.Title, &task.Date, &task.Comment, &task.Repeat)
	if err != nil {
		if err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(TaskResponse{Error: "Задача не найдена"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка получения задачи"})
		}
		return
	}
	json.NewEncoder(w).Encode(task)
}

func handleDeleteTask(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	taskIdStr := r.URL.Query().Get("id")
	if taskIdStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Не указан ID задачи"})
		return
	}

	taskId, err := strconv.Atoi(taskIdStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Некорректный формат ID задачи"})
		return
	}

	res, err := db.Exec("DELETE FROM scheduler WHERE id = ?", taskId)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка удаления задачи"})
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка получения результатов удаления"})
		return
	}
	if rowsAffected == 0 {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Задача не найдена"})
		return
	}

	json.NewEncoder(w).Encode(TaskResponse{ID: taskId})
}

func handleGetTasks(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")

	const limitDefault = 50

	search := r.URL.Query().Get("search")
	search = strings.TrimSpace(search)

	var (
		query string
		args  []any
	)

	if search == "" {
		// без параметра search — все задачи, сортируем и лимитируем
		query = `
			SELECT id, date, title, comment, repeat
			FROM scheduler
			ORDER BY date ASC
			LIMIT ?
		`
		args = append(args, limitDefault)
	} else {
		// проверяем, не является ли search датой формата dd.mm.yyyy
		parsedDate, err := time.Parse("02.01.2006", search)
		if err == nil {
			// значит ищем конкретную дату
			dateForDB := parsedDate.Format("20060102")
			query = `
				SELECT id, date, title, comment, repeat
				FROM scheduler
				WHERE date = ?
				ORDER BY date ASC
				LIMIT ?
			`
			args = append(args, dateForDB, limitDefault)
		} else {
			// ищем подстроку в title или comment
			likePattern := fmt.Sprintf("%%%s%%", search)
			query = `
				SELECT id, date, title, comment, repeat
				FROM scheduler
				WHERE title LIKE ? OR comment LIKE ?
				ORDER BY date ASC
				LIMIT ?
			`
			args = append(args, likePattern, likePattern, limitDefault)
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Println("Ошибка при запросе списка задач:", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Ошибка получения списка задач"})
		return
	}
	defer rows.Close()

	var tasks []TaskForJSON

	for rows.Next() {
		var (
			id      int
			dateStr string
			title   string
			comment string
			repeat  string
		)

		if err := rows.Scan(&id, &dateStr, &title, &comment, &repeat); err != nil {
			log.Println("Ошибка чтения данных задачи:", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Ошибка чтения задач из базы"})
			return
		}

		tasks = append(tasks, TaskForJSON{
			ID:      strconv.Itoa(id),
			Date:    dateStr,
			Title:   title,
			Comment: comment,
			Repeat:  repeat,
		})
	}
	if err := rows.Err(); err != nil {
		log.Println("Ошибка итерации по строкам:", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Ошибка итерации по задачам"})
		return
	}

	// Если ничего не нашли, tasks == nil, сделаем пустой срез
	if tasks == nil {
		tasks = []TaskForJSON{}
	}

	// Формируем результат
	result := map[string]any{
		"tasks": tasks,
	}
	json.NewEncoder(w).Encode(result)
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
		for !nextDate.After(now) {
			nextDate = nextDate.AddDate(0, 0, days)
		}

	case repeat == "y":
		for !nextDate.After(now) {
			if nextDate.Month() == 2 && nextDate.Day() == 29 {
				nextDate = nextDate.AddDate(1, 0, 0)
				if !isLeapYear(nextDate.Year()) {
					nextDate = time.Date(
						nextDate.Year(), 2, 28,
						0, 0, 0, 0, nextDate.Location(),
					)
				}
			} else {
				nextDate = nextDate.AddDate(1, 0, 0)
				// обработка 31-го числа, если нужно
				if parsedDate.Day() == 31 {
					nextDate = time.Date(
						nextDate.Year(), nextDate.Month()+1, 0,
						0, 0, 0, 0, nextDate.Location(),
					)
				}
			}
		}

	default:
		return "", fmt.Errorf("неподдерживаемый формат: %s", repeat)
	}

	return nextDate.Format("20060102"), nil
}

func isLeapYear(year int) bool {
	return (year%4 == 0 && year%100 != 0) || (year%400 == 0)
}
