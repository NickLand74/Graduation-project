package main

import (
	"database/sql"
	"encoding/json"
	"errors"
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

// Task — структура для приёма и сохранения задач.
// Поле ID не было раньше, но для обновления задачи оно необходимо.
type Task struct {
	ID      int    `json:"id,omitempty"` // при добавлении не нужен, при обновлении — обязателен
	Date    string `json:"date"`         // YYYYMMDD
	Title   string `json:"title"`
	Comment string `json:"comment,omitempty"`
	Repeat  string `json:"repeat,omitempty"`
}

// TaskResponse — для ответов при ошибках или при создании задачи.
type TaskResponse struct {
	ID    int    `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}

// TaskDetail — структура для возврата задачи целиком (с ID в виде строки).
// Можно было бы вернуть ID как int, но тесты (и фронт) обычно ожидают строку.
type TaskDetail struct {
	ID      string `json:"id"` // строка, чтобы JSON всегда был в нужном формате
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

	// Регистрация маршрута /api/task
	http.HandleFunc("/api/task", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")

		// Обработка различных HTTP-методов через switch
		switch r.Method {
		case http.MethodPost:
			handleAddTask(w, r, db) // для POST — добавление задачи
		case http.MethodGet:
			handleGetTask(w, r, db) // для GET — получение информации о задаче
		case http.MethodPut:
			handleUpdateTask(w, r, db) // для PUT — обновление задачи
		case http.MethodDelete:
			handleDeleteTask(w, r, db) // для DELETE — удаление задачи
		default:
			// Если метод не поддерживается — возвращаем ошибку 405
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(TaskResponse{Error: "Метод не поддерживается"})
		}
	})

	// Дополнительный маршрут /api/tasks для работы со списком задач (поиск и фильтры)
	http.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			// Метод не поддерживается
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]string{"error": "Метод не поддерживается"})
			return
		}
		handleGetTasks(w, r, db)
	})

	// Регистрация маршрута для /api/nextdate
	http.HandleFunc("/api/nextdate", handleNextDate)

	// Регистрация маршрута /api/task/done — POST для отметки о выполнении
	http.HandleFunc("/api/task/done", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handleDoneTask(w, r, db)
		} else {
			// На все остальные методы отдаём 404 (или 405)
			http.NotFound(w, r)
		}
	})

	// Запуск HTTP-сервера на указанном порту
	fmt.Printf("Сервер запущен на http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Println("Ошибка запуска сервера:", err)
	}
}

// Создание таблицы, если ещё не создана
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

// Добавление новой задачи (POST)
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
			// Если нет повторения, просто устанавливаем дату на сегодня
			task.Date = today.Format("20060102")
		} else {
			// Если есть повторение, двигаем дату с помощью NextDate
			nextDate, err := NextDate(today, task.Date, task.Repeat)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(TaskResponse{Error: "Неверное правило повторения"})
				return
			}
			task.Date = nextDate
		}
	}

	res, err := db.Exec(`
		INSERT INTO scheduler (title, date, comment, repeat) 
		VALUES (?, ?, ?, ?)`,
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

// Получение одной задачи (GET /api/task?id=...)
func handleGetTask(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	taskID := r.URL.Query().Get("id")
	if taskID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Не указан идентификатор"})
		return
	}

	var (
		id      int
		date    string
		title   string
		comment string
		repeat  string
	)

	err := db.QueryRow(`
		SELECT id, date, title, comment, repeat 
		FROM scheduler 
		WHERE id = ?`, taskID).
		Scan(&id, &date, &title, &comment, &repeat)

	if err != nil {
		if err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Задача не найдена"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Ошибка получения задачи"})
		}
		return
	}

	// Возвращаем в удобном формате JSON с ID как строкой
	td := TaskDetail{
		ID:      strconv.Itoa(id),
		Date:    date,
		Title:   title,
		Comment: comment,
		Repeat:  repeat,
	}

	json.NewEncoder(w).Encode(td)
}

type UpdateRequest struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Title   string `json:"title"`
	Comment string `json:"comment"`
	Repeat  string `json:"repeat"`
}

func handleUpdateTask(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Ошибка десериализации
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка десериализации JSON"})
		return
	}

	// Попробуем сконвертировать req.ID в число
	idNum, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil || idNum <= 0 {
		// если в ID не число > 0 => "Не указан идентификатор задачи"
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Не указан идентификатор задачи"})
		return
	}

	// Теперь соберём структуру Task (у вас в БД int, значит приводим к int)
	incoming := Task{
		ID:      int(idNum),
		Date:    req.Date,
		Title:   req.Title,
		Comment: req.Comment,
		Repeat:  req.Repeat,
	}

	// 2. Посмотрим, что реально пришло
	log.Printf("DEBUG: incoming => ID=%d, Date=%q, Title=%q, Comment=%q, Repeat=%q\n",
		incoming.ID, incoming.Date, incoming.Title, incoming.Comment, incoming.Repeat)

	// 3. Проверяем ID
	if incoming.ID <= 0 {
		log.Println("DEBUG: ID is 0 or negative => returning error")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Не указан идентификатор задачи"})
		return
	}

	// 4. Проверяем title
	if incoming.Title == "" {
		log.Println("DEBUG: title is empty => returning error")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Не указан заголовок задачи"})
		return
	}

	// 5. Если дата пустая => ставим today's date (по условию теста)
	if incoming.Date == "" {
		incoming.Date = time.Now().Format("20060102")
		log.Printf("DEBUG: date was empty => set to today %q\n", incoming.Date)
	}

	// 6. Парсим дату
	parsedDate, err := time.Parse("20060102", incoming.Date)
	if err != nil {
		log.Printf("DEBUG: invalid date => %q\n", incoming.Date)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Неверный формат даты"})
		return
	}

	// 7. Проверяем, что дата >= сегодня
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if parsedDate.Before(today) {
		log.Printf("DEBUG: date < today => returning error, date=%q\n", incoming.Date)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Дата не может быть меньше сегодняшней"})
		return
	}

	// 8. Проверяем repeat, если не пустой
	if incoming.Repeat != "" {
		if strings.HasPrefix(incoming.Repeat, "d ") {
			daysStr := strings.TrimSpace(strings.TrimPrefix(incoming.Repeat, "d "))
			days, err := strconv.Atoi(daysStr)
			if err != nil || days <= 0 {
				log.Printf("DEBUG: bad repeat => %q\n", incoming.Repeat)
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(TaskResponse{Error: "Неверное правило повторения"})
				return
			}
		} else if incoming.Repeat != "y" {
			log.Printf("DEBUG: bad repeat => %q\n", incoming.Repeat)
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(TaskResponse{Error: "Неверное правило повторения"})
			return
		}
	}

	// 9. Если все проверки пройдены => идём делать UPDATE
	log.Printf("DEBUG: going to UPDATE. ID=%d Title=%q Date=%q Comment=%q Repeat=%q",
		incoming.ID, incoming.Title, incoming.Date, incoming.Comment, incoming.Repeat)

	res, err := db.Exec(`
        UPDATE scheduler
        SET title=?, date=?, comment=?, repeat=?
        WHERE id=?
    `, incoming.Title, incoming.Date, incoming.Comment, incoming.Repeat, incoming.ID)
	if err != nil {
		log.Printf("ERROR: db.Exec failed: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка обновления задачи в базе данных"})
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		log.Printf("ERROR: rowsAffected error: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка получения результата обновления"})
		return
	}

	if rowsAffected == 0 {
		log.Printf("DEBUG: rowsAffected == 0 => id not found = %d\n", incoming.ID)
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Задача не найдена"})
		return
	}

	// 10. Всё ок
	log.Println("DEBUG: UPDATE success => returning empty JSON")
	json.NewEncoder(w).Encode(map[string]any{})
}

// удаление задачи
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

	// Пытаемся удалить
	res, err := db.Exec("DELETE FROM scheduler WHERE id = ?", taskId)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка удаления задачи"})
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка получения результата удаления"})
		return
	}
	if rowsAffected == 0 {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Задача не найдена"})
		return
	}

	// Если всё ОК — возвращаем пустой JSON
	json.NewEncoder(w).Encode(map[string]any{})
}

// handleDoneTask обрабатывает POST /api/task/done?id=<id>
func handleDoneTask(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	// 1. Получаем ID задачи из query
	taskIdStr := r.URL.Query().Get("id")
	if taskIdStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Не указан ID задачи"})
		return
	}

	taskID, err := strconv.Atoi(taskIdStr)
	if err != nil || taskID <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Некорректный формат ID задачи"})
		return
	}

	// 2. Читаем задачу из БД
	var t Task
	err = db.QueryRow(`
        SELECT id, date, title, comment, repeat 
        FROM scheduler 
        WHERE id = ?
    `, taskID).Scan(&t.ID, &t.Date, &t.Title, &t.Comment, &t.Repeat)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(TaskResponse{Error: "Задача не найдена"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка чтения задачи из БД"})
		}
		return
	}

	// 3. Если repeat пустой => задача одноразовая => УДАЛЯЕМ
	if t.Repeat == "" {
		// удаляем
		_, err := db.Exec("DELETE FROM scheduler WHERE id = ?", taskID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка удаления задачи из БД"})
			return
		}
		// Возвращаем пустой JSON {}
		json.NewEncoder(w).Encode(map[string]any{})
		return
	}

	// 4. Иначе задача периодическая => считаем новую дату через NextDate()
	now := time.Now()
	newDate, err := NextDate(now, t.Date, t.Repeat)
	if err != nil {
		// Если по каким-то причинам NextDate не смогла вычислить (например, repeat кривой),
		// вернём ошибку. Хотя по тестам это вряд ли случится, так как repeat уже валидный.
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка вычисления даты повторения"})
		return
	}

	// 5. Обновляем date в БД
	_, err = db.Exec(`
        UPDATE scheduler 
        SET date = ? 
        WHERE id = ?
    `, newDate, taskID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TaskResponse{Error: "Ошибка обновления задачи в БД"})
		return
	}

	// Успех => пустой JSON {}
	json.NewEncoder(w).Encode(map[string]any{})
}

// handleGetTasks — получение списка задач (сортировка, поиск и т. д.)
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
		// без параметра search — все задачи
		query = `
			SELECT id, date, title, comment, repeat
			FROM scheduler
			ORDER BY date ASC
			LIMIT ?
		`
		args = append(args, limitDefault)
	} else {
		// проверим, не является ли search датой формата dd.mm.yyyy
		parsedDate, err := time.Parse("02.01.2006", search)
		if err == nil {
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

	var tasks []TaskDetail

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

		tasks = append(tasks, TaskDetail{
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

	// Если ничего не нашли, tasks == nil => пустой срез
	if tasks == nil {
		tasks = []TaskDetail{}
	}

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

		// ШАГ 1: всегда двигаем дату "хотя бы на 1 раз"
		nextDate = nextDate.AddDate(0, 0, days)

		// ШАГ 2: пока nextDate <= now, двигаем ещё
		for !nextDate.After(now) {
			nextDate = nextDate.AddDate(0, 0, days)
		}

	case repeat == "y":
		// Сначала "один шаг" вперёд от исходной даты
		if nextDate.Month() == 2 && nextDate.Day() == 29 {
			// если это было 29 февраля, добавим год:
			nextDate = nextDate.AddDate(1, 0, 0)
			if !isLeapYear(nextDate.Year()) {
				// смещаемся на 1 марта, если год не високосный
				nextDate = time.Date(
					nextDate.Year(), 3, 1,
					0, 0, 0, 0, nextDate.Location(),
				)
			}
		} else {
			// обычный год
			nextDate = nextDate.AddDate(1, 0, 0)
			// Если день был 31, можно применить логику "не вывалиться из месяца"
			if parsedDate.Day() == 31 {
				nextDate = time.Date(
					nextDate.Year(), nextDate.Month()+1, 0,
					0, 0, 0, 0, nextDate.Location(),
				)
			}
		}

		// А теперь добавляем ещё год, пока дата не станет > now
		for !nextDate.After(now) {
			// повторяем ту же логику 29 февраля / 31-е
			if nextDate.Month() == 2 && nextDate.Day() == 29 {
				nextDate = nextDate.AddDate(1, 0, 0)
				if !isLeapYear(nextDate.Year()) {
					nextDate = time.Date(
						nextDate.Year(), 3, 1,
						0, 0, 0, 0, nextDate.Location(),
					)
				}
			} else {
				nextDate = nextDate.AddDate(1, 0, 0)
				if nextDate.Day() == 31 {
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

// isLeapYear — проверяет, является ли год високосным
func isLeapYear(year int) bool {
	return (year%4 == 0 && year%100 != 0) || (year%400 == 0)
}

func handleNextDate(w http.ResponseWriter, r *http.Request) {
	// Тест ожидает обычный текст (не JSON):
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	nowStr := r.URL.Query().Get("now")
	dateStr := r.URL.Query().Get("date")
	repeatStr := r.URL.Query().Get("repeat")

	// Парсим nowStr как "20060102"
	nowTime, err := time.Parse("20060102", nowStr)
	if err != nil {
		// Если невалидная дата now => возвращаем пустую строку
		// (согласно логике теста "если ошибка => пустая строка")
		w.Write([]byte(""))
		return
	}

	// Вызываем вашу функцию NextDate
	next, err := NextDate(nowTime, dateStr, repeatStr)
	if err != nil {
		// Если NextDate вернула ошибку => тоже пустую строку
		w.Write([]byte(""))
		return
	}

	// Если всё ок, возвращаем найденную дату
	w.Write([]byte(next))
}
