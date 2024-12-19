package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

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
	fmt.Printf("Сервер запущен на :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Println("Ошибка запуска сервера:", err)
	}

	// Получение пути к исполняемому файлу
	appPath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}

	// Определение пути к файлу базы данных
	dbFile := filepath.Join(filepath.Dir(appPath), "scheduler.db")

	// Проверка существования файла базы данных
	_, err = os.Stat(dbFile)

	var install bool
	if err != nil {
		install = true
	}

	// Открытие соединения с базой данных
	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Если install равно true, создаем таблицы и индексы
	if install {
		// SQL-запрос для создания таблицы
		createTableSQL := `CREATE TABLE IF NOT EXISTS tasks (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            completed BOOLEAN NOT NULL DEFAULT FALSE
        );`

		// Выполняем запрос
		if _, err := db.Exec(createTableSQL); err != nil {
			log.Fatal(err)
		}

		// Здесь можно добавить запросы для создания индексов, если это нужно
		fmt.Println("База данных создана и таблицы добавлены.")
	} else {
		fmt.Println("База данных уже существует.")
	}
}
