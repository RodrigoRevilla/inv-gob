package main

import (
	"log"
	"net/http"

	"inv-gob/backend/config"
	"inv-gob/backend/db"
	"inv-gob/backend/handlers"
	mw "inv-gob/backend/middleware"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func main() {
	cfg := config.Load()

	if err := db.Connect(cfg.DBUrl); err != nil {
		log.Fatalf("Error conectando a DB: %v", err)
	}
	defer db.Close()

	mw.SetJWTSecret(cfg.JWTSecret)

	r := chi.NewRouter()

	r.Use(chimw.Recoverer)
	r.Use(mw.Logger)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"service":"inv-gov-api"}`))
	})

	r.Post("/api/v1/auth/login", handlers.Login)

	r.Group(func(r chi.Router) {
		r.Use(mw.Auth)
		r.With(mw.RequiereRol("admin")).Post("/api/v1/usuarios", handlers.CrearUsuario)
		r.With(mw.RequiereRol("admin")).Get("/api/v1/usuarios", handlers.ListarUsuarios)
		r.With(mw.RequiereRol("admin")).Post("/api/v1/catalogo/importar", handlers.ImportarCatalogo)
		r.Get("/api/v1/catalogo", handlers.ListarCatalogo)
		r.With(mw.RequiereRol("supervisor", "admin")).Post("/api/v1/sesiones", handlers.CrearSesion)
		r.Get("/api/v1/sesiones", handlers.ListarSesiones)
		r.Get("/api/v1/sesiones/{id}", handlers.ObtenerSesion)
		r.With(mw.RequiereRol("supervisor", "admin")).Patch("/api/v1/sesiones/{id}/pausar", handlers.PausarSesion)
		r.With(mw.RequiereRol("supervisor", "admin")).Patch("/api/v1/sesiones/{id}/reanudar", handlers.ReanudarSesion)
		r.With(mw.RequiereRol("supervisor", "admin")).Post("/api/v1/sesiones/{id}/cerrar", handlers.CerrarSesion)
		r.Get("/api/v1/sesiones/{id}/faltantes", handlers.ListarFaltantes)
		r.With(mw.RequiereRol("escaner", "supervisor", "admin")).Post("/api/v1/escaneos", handlers.RegistrarEscaneo)
		r.Get("/api/v1/escaneos", handlers.ListarEscaneos)
		r.With(mw.RequiereRol("admin")).Patch("/api/v1/usuarios/{id}/toggle", handlers.ToggleUsuario)
		r.With(mw.RequiereRol("admin")).Patch("/api/v1/usuarios/{id}/password", handlers.ResetPassword)
		r.With(mw.RequiereRol("admin", "auditor")).Get("/api/v1/audit-log", handlers.ListarAuditLog)
	})

	log.Printf("✓ API corriendo en http://localhost:%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, r))
}
