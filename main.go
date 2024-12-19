package main

import (
	"fmt"
	"net/http"
	"os"
)

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello, world!")
}

func main() {
	port := os.Getenv("TODO_PORT")
	if port == "" {
		port = "7540"
	}

	http.HandleFunc("/", handler)
	fmt.Printf("Сервер запущен на :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Println("Ошибка запуска сервера:", err)
	}
}
