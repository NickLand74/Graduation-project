package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ==========================
// 1) Тесты makeJWT / validateJWT
// ==========================

func TestMakeJWT(t *testing.T) {
	token, err := makeJWT("1234")
	if err != nil {
		t.Errorf("makeJWT вернул ошибку: %v", err)
	}
	if token == "" {
		t.Error("makeJWT вернул пустой токен")
	}

	// Проверка: makeJWT(пустой пароль) => ошибка
	badToken, err := makeJWT("")
	if err == nil {
		t.Error("ожидалась ошибка при пустом пароле, но err=nil")
	}
	if badToken != "" {
		t.Error("при пустом пароле токен должен быть пустым")
	}
}

func TestValidateJWT(t *testing.T) {
	// Генерируем токен для пароля "xyz"
	token, err := makeJWT("xyz")
	if err != nil {
		t.Fatalf("makeJWT для 'xyz' вернул ошибку: %v", err)
	}

	// Должен быть валиден при validateJWT(token, "xyz")
	if !validateJWT(token, "xyz") {
		t.Error("token для 'xyz' оказался невалидным при проверке с тем же паролем")
	}

	// Не должен быть валиден при другом пароле
	if validateJWT(token, "1234") {
		t.Error("token для 'xyz' оказался валидным при проверке с другим паролем")
	}

	// Проверим кривой токен
	if validateJWT("подделка.123.456", "xyz") {
		t.Error("подделка токена не должна валидироваться")
	}
}

func TestMakePasswordHash(t *testing.T) {
	hash1 := makePasswordHash("abc")
	hash2 := makePasswordHash("abc")
	if hash1 != hash2 {
		t.Error("хеш для одинаковых паролей должен совпадать")
	}

	// Хеш для другого пароля должен отличаться
	hash3 := makePasswordHash("def")
	if hash1 == hash3 {
		t.Error("хеш для разных паролей не должен совпадать")
	}

	// Проверим, что это действительно sha256(пароль + salt)
	salt := "Salt777"
	want := sha256.Sum256([]byte("abc" + salt))
	wantStr := base64.RawURLEncoding.EncodeToString(want[:])
	if hash1 != wantStr {
		t.Errorf("хеш не совпадает с ожидаемым: got=%s, want=%s", hash1, wantStr)
	}
}

// ==========================
// 2) Тест middleware auth
// ==========================

func TestAuthMiddleware(t *testing.T) {
	// Оборачиваем простой хендлер, который пишет "OK" при успехе
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	// Создаём middleware
	wrapped := auth(nextHandler)

	t.Run("нет пароля в окружении => пропускаем без проверки", func(t *testing.T) {
		os.Setenv("TODO_PASSWORD", "") // убираем пароль
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("ожидали 200 OK, получили %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "OK") {
			t.Error("ожидали в ответе 'OK', а получили:", rr.Body.String())
		}
	})

	t.Run("пароль задан, но нет куки => 401", func(t *testing.T) {
		os.Setenv("TODO_PASSWORD", "1234")
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("ожидали 401, получили %d", rr.Code)
		}
	})

	t.Run("пароль задан, невалидный токен => 401", func(t *testing.T) {
		os.Setenv("TODO_PASSWORD", "1234")
		req := httptest.NewRequest("GET", "/", nil)
		// Ставим поддельный токен
		req.AddCookie(&http.Cookie{Name: "token", Value: "подделка.подделка.подделка"})
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("ожидали 401 при неверном токене, получили %d", rr.Code)
		}
	})

	t.Run("пароль задан, валидный токен => 200 OK", func(t *testing.T) {
		os.Setenv("TODO_PASSWORD", "1234")
		goodToken, err := makeJWT("1234")
		if err != nil {
			t.Fatal("не удалось сделать токен для 1234:", err)
		}

		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "token", Value: goodToken})
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("ожидали 200, получили %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "OK") {
			t.Errorf("в теле ожидали 'OK', а получили: %s", rr.Body.String())
		}
	})
}

// ==========================
// 3) Тест handleSignin
// ==========================

func TestHandleSignin(t *testing.T) {
	// Подготовим хендлер
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleSignin(w, r)
	})

	t.Run("пароль не установлен => вернуть ошибку", func(t *testing.T) {
		os.Setenv("TODO_PASSWORD", "")
		reqBody := strings.NewReader(`{"password":"1234"}`)
		req := httptest.NewRequest("POST", "/api/signin", reqBody)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("ожидали код 200, получили %d", rr.Code)
		}

		// Парсим JSON
		var resp SigninResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Errorf("ошибка разбора JSON: %v", err)
		}
		if !strings.Contains(resp.Error, "Пароль не установлен") {
			t.Errorf("ожидали ошибку про пустой пароль, а получили: %s", resp.Error)
		}
	})

	t.Run("неверный пароль => 401", func(t *testing.T) {
		os.Setenv("TODO_PASSWORD", "1234")
		reqBody := strings.NewReader(`{"password":"qwerty"}`)
		req := httptest.NewRequest("POST", "/api/signin", reqBody)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("ожидали 401, получили %d", rr.Code)
		}

		var resp SigninResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Errorf("ошибка разбора JSON: %v", err)
		}
		if !strings.Contains(resp.Error, "Неверный пароль") {
			t.Errorf("ожидали 'Неверный пароль', а получили: %s", resp.Error)
		}
	})

	t.Run("верный пароль => вернуть token", func(t *testing.T) {
		os.Setenv("TODO_PASSWORD", "1234")
		reqBody := strings.NewReader(`{"password":"1234"}`)
		req := httptest.NewRequest("POST", "/api/signin", reqBody)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("ожидали 200 OK, получили %d", rr.Code)
		}

		var resp SigninResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Errorf("ошибка разбора JSON: %v", err)
		}
		if resp.Error != "" {
			t.Errorf("не ожидали error, но получили: %s", resp.Error)
		}
		if resp.Token == "" {
			t.Error("ожидали токен, но resp.Token пустой")
		}
		// Дополнительно можно проверить validateJWT(resp.Token, "1234")==true
		if !validateJWT(resp.Token, "1234") {
			t.Error("сгенерированный токен оказался невалидным")
		}
	})

	t.Run("ошибка десериализации JSON => 400", func(t *testing.T) {
		os.Setenv("TODO_PASSWORD", "1234")
		// Отправим кривой JSON
		reqBody := strings.NewReader(`{bad json}`)
		req := httptest.NewRequest("POST", "/api/signin", reqBody)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("ожидали 400, получили %d", rr.Code)
		}
	})
}
