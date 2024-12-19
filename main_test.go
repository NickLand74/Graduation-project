package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
