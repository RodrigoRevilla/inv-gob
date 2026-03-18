package db

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func Connect(dbUrl string) error {
	var err error
	Pool, err = pgxpool.New(context.Background(), dbUrl)
	if err != nil {
		return fmt.Errorf("no se pudo conectar a la base de datos: %w", err)
	}

	if err = Pool.Ping(context.Background()); err != nil {
		return fmt.Errorf("ping fallido a la base de datos: %w", err)
	}

	log.Println("✓ Conexión a PostgreSQL establecida")
	return nil
}

func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
