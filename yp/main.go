package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"

	_ "github.com/lib/pq"
	"github.com/sidereusnuntius/yp/app"
)

//go:embed schema.sql
var schemaSQL string

func initDB() (*sql.DB, error) {
	connStr := os.Getenv("DATABASE_URL")

	if connStr == "" {
		// Fallback para teste local — substitua pela sua string de conexão
		connStr = "postgresql://yp_tcc_db_user:epyYzvka62bMvalz5Y79lE9vAYg3RhVW@dpg-d7l6sc7lk1mc73b0bo6g-a.oregon-postgres.render.com/yp_tcc_db"
	}

	if !strings.Contains(connStr, "sslmode=") {
		if strings.Contains(connStr, "?") {
			connStr += "&sslmode=require"
		} else {
			connStr += "?sslmode=require"
		}
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

func setupDatabase(db *sql.DB) error {
	slog.Info("Configurando banco de dados...")
	_, err := db.Exec(schemaSQL)
	if err != nil {
		return fmt.Errorf("erro ao executar schema: %v", err)
	}
	slog.Info("Banco configurado com sucesso.")
	return nil
}

func main() {
	db, err := initDB()
	if err != nil {
		slog.Error("Falha na conexão com o banco: " + err.Error())
		os.Exit(1)
	}

	if err = setupDatabase(db); err != nil {
		slog.Error("Falha ao rodar schema: " + err.Error())
		os.Exit(1)
	}

	s, err := app.InitServer(db)
	if err != nil {
		slog.Error("Falha ao iniciar servidor: " + err.Error())
		os.Exit(1)
	}

	slog.Info("Servidor ouvindo na porta 8080.")
	s.ListenAndServe()
}
