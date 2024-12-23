package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestHandler(t *testing.T) {
	// Создаем новый запрос
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Запускаем тестовый сервер
	rec := httptest.NewRecorder()
	handler := http.HandlerFunc(Handler)

	// Вызываем обработчик
	handler.ServeHTTP(rec, req)

	// Проверяем статус-код
	if status := rec.Code; status != http.StatusOK {
		t.Errorf("Неверный статус-код: ожидался %v, получен %v", http.StatusOK, status)
	}

	// Проверяем тело ответа
	expected := "Hello, world!\n"
	if rec.Body.String() != expected {
		t.Errorf("Неверное тело ответа: ожидалось %v, получено %v", expected, rec.Body.String())
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
		{"20240126", "20240229", "y", "20250228", false}, // 29 февраля - високосный год
		{"20240226", "20240229", "y", "20260229", false}, // 29 февраля - високосный год
		{"20240126", "20240131", "d 1", "20240201", false},
		{"20240126", "20240131", "y", "20250131", false},
		{"20240131", "20240229", "y", "20250228", false}, // следующий год не високосный
		{"20240131", "20240228", "y", "20250228", false}, // следующий год не високосный
		{"20240126", "20240126", "d 405", "", true},      // превышение лимита
		{"20240126", "20240126", "", "", true},           // пустое правило повторения
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
