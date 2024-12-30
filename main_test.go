package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestHandler(t *testing.T) {
	// Создаем временные каталоги и файлы для тестирования
	os.MkdirAll("web/js", os.ModePerm)
	os.MkdirAll("web/css", os.ModePerm)
	defer os.RemoveAll("web") // Удаляем временные файлы после теста

	// Создаем файл index.html
	indexHTML := []byte("<html><head><link rel=\"stylesheet\" type=\"text/css\" href=\"/css/style.css\"></head><body><h1>Hello, world!</h1><script src=\"/js/scripts.min.js\"></script></body></html>")
	if err := os.WriteFile("web/index.html", indexHTML, 0644); err != nil {
		t.Fatal(err)
	}

	// Создаем файл style.css
	cssContent := []byte("body { background-color: #fff; }")
	if err := os.WriteFile("web/css/style.css", cssContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Создаем файл scripts.min.js
	jsContent := []byte("console.log('Hello, world!');")
	if err := os.WriteFile("web/js/scripts.min.js", jsContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Запускаем тестовый сервер
	rec := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := http.FileServer(http.Dir("./web"))
	handler.ServeHTTP(rec, req)

	// Проверяем статус-код для index.html
	if status := rec.Code; status != http.StatusOK {
		t.Errorf("Неверный статус-код для index.html: ожидается %v, получен %v", http.StatusOK, status)
	}

	// Проверяем тело ответа для index.html
	expectedBody := "<html><head><link rel=\"stylesheet\" type=\"text/css\" href=\"/css/style.css\"></head><body><h1>Hello, world!</h1><script src=\"/js/scripts.min.js\"></script></body></html>"
	if strings.TrimSpace(rec.Body.String()) != expectedBody {
		t.Errorf("Неверное тело ответа для index.html: ожидалось %v, получено %v", expectedBody, rec.Body.String())
	}

	// Тест для проверки файла style.css
	rec = httptest.NewRecorder()
	req, err = http.NewRequest("GET", "/css/style.css", nil)
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(rec, req)

	if status := rec.Code; status != http.StatusOK {
		t.Errorf("Неверный статус-код для style.css: ожидается %v, получен %v", http.StatusOK, status)
	}
	if rec.Body.String() != string(cssContent) {
		t.Errorf("Неверное тело ответа для style.css: ожидалось %v, получено %v", string(cssContent), rec.Body.String())
	}

	// Тест для проверки файла scripts.min.js
	rec = httptest.NewRecorder()
	req, err = http.NewRequest("GET", "/js/scripts.min.js", nil)
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(rec, req)

	if status := rec.Code; status != http.StatusOK {
		t.Errorf("Неверный статус-код для scripts.min.js: ожидается %v, получен %v", http.StatusOK, status)
	}
	if rec.Body.String() != string(jsContent) {
		t.Errorf("Неверное тело ответа для scripts.min.js: ожидалось %v, получено %v", string(jsContent), rec.Body.String())
	}
}

func TestDatabaseInitialization(t *testing.T) {
	// Получаем путь к исполняемому файлу
	appPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dbFile := filepath.Join(filepath.Dir(appPath), "scheduler.db")

	// Удаляем файл базы данных, если он существует
	os.Remove(dbFile)

	// Открытие соединения с базой данных
	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Проверяем существует ли база данных
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tasks';").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	// Ожидаем, что таблица 'tasks' не существует
	if count != 0 {
		t.Errorf("Таблица 'tasks' должна отсутствовать, но была найдена.")
	}

	// Создание таблицы
	createTableSQL := `CREATE TABLE IF NOT EXISTS tasks (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        completed BOOLEAN NOT NULL DEFAULT FALSE
    );`

	if _, err := db.Exec(createTableSQL); err != nil {
		t.Fatal(err)
	}

	// Проверяем существует ли таблица после создания
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tasks';").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	// Ожидаем, что таблица 'tasks' теперь должна существовать
	if count != 1 {
		t.Errorf("Таблица 'tasks' должна существовать, но не найдена.")
	}

	// Удаляем файл после завершения теста
	os.Remove(dbFile)
}
func TestNextDate(t *testing.T) {
	tests := []struct {
		now      string
		date     string
		repeat   string
		expected string
		hasError bool
	}{
		{"20240126", "20240126", "d 1", "20240127", false},
		{"20240126", "20240126", "d 7", "20240202", false},
		{"20240126", "20240229", "y", "20250228", false}, // 29 февраля не будет в 2025 году
		{"20240126", "20240131", "d 1", "20240201", false},
		{"20240126", "20240131", "y", "20250131", false},
		{"20240131", "20240229", "y", "20250228", false},
		{"20240131", "20240228", "y", "20250228", false},
		{"20240126", "20240126", "d 405", "", true}, // превышение лимита
		{"20240126", "20240126", "", "", true},      // пустое правило повторения
	}

	for _, tt := range tests {
		now, _ := time.Parse("20060102", tt.now)
		nextDate, err := NextDate(now, tt.date, tt.repeat)

		if tt.hasError {
			if err == nil {
				t.Errorf("Expected an error for input (%s, %s, %s), got nil", tt.now, tt.date, tt.repeat)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error for input (%s, %s, %s): %v", tt.now, tt.date, tt.repeat, err)
			}
			if nextDate != tt.expected {
				t.Errorf("Expected next date %s, got %s for input (%s, %s, %s)", tt.expected, nextDate, tt.now, tt.date, tt.repeat)
			}
		}
	}
}
