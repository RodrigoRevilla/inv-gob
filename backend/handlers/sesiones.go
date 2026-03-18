package handlers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"inv-gob/backend/db"
	mw "inv-gob/backend/middleware"
	"inv-gob/backend/models"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func CrearSesion(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)

	var req models.CrearSesionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.Err("BODY_INVALIDO", "JSON inválido"))
		return
	}

	if req.NombreSesion == "" {
		writeJSON(w, http.StatusBadRequest, models.Err("NOMBRE_REQUERIDO", "El nombre de la sesión es requerido"))
		return
	}

	var versionID string
	err := db.Pool.QueryRow(r.Context(), `
		SELECT id FROM versiones_catalogo
		WHERE dependencia_id = $1 AND activa = TRUE
	`, claims.DependenciaID).Scan(&versionID)

	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusBadRequest, models.Err("SIN_CATALOGO", "No hay catálogo activo. Importa el catálogo primero."))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al verificar catálogo"))
		return
	}

	var id string
	err = db.Pool.QueryRow(r.Context(), `
		INSERT INTO sesiones_escaneo (dependencia_id, version_id, iniciado_por, nombre_sesion, descripcion, area_cubierta)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, claims.DependenciaID, versionID, claims.UsuarioID,
		req.NombreSesion, req.Descripcion, req.AreaCubierta).Scan(&id)

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al crear sesión"))
		return
	}

	registrarAudit(r.Context(), claims.UsuarioID, id, "sesiones_escaneo", "ABRIR_SESION", nil, map[string]string{
		"nombre_sesion": req.NombreSesion,
		"area_cubierta": req.AreaCubierta,
	})

	writeJSON(w, http.StatusCreated, models.OK(map[string]string{"id": id}))
}

func ListarSesiones(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)

	rows, err := db.Pool.Query(r.Context(), `
    SELECT sesion_id, nombre_sesion, estado, iniciada_at, cerrada_at,
           iniciado_por, total_en_catalogo, total_escaneados,
           coincidencias, ubicacion_diferente, no_en_catalogo, faltantes
    FROM v_resumen_sesion
    WHERE dependencia_id = $1
    ORDER BY iniciada_at DESC
    LIMIT 50
`, claims.DependenciaID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al listar sesiones"))
		return
	}
	defer rows.Close()

	var sesiones []models.ResumenSesion
	for rows.Next() {
		var s models.ResumenSesion
		if err := rows.Scan(
			&s.SesionID, &s.NombreSesion, &s.Estado, &s.IniciodaAt, &s.CerradaAt,
			&s.IniciodoPor, &s.TotalEnCatalogo, &s.TotalEscaneados,
			&s.Coincidencias, &s.UbicacionDiferente, &s.NoEnCatalogo, &s.Faltantes,
		); err != nil {
			continue
		}
		sesiones = append(sesiones, s)
	}

	writeJSON(w, http.StatusOK, models.OK(sesiones))
}

func ObtenerSesion(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)
	sesionID := chi.URLParam(r, "id")

	var s models.ResumenSesion
	err := db.Pool.QueryRow(r.Context(), `
    SELECT sesion_id, nombre_sesion, estado, iniciada_at, cerrada_at,
           iniciado_por, total_en_catalogo, total_escaneados,
           coincidencias, ubicacion_diferente, no_en_catalogo, faltantes
    FROM v_resumen_sesion
    WHERE sesion_id = $1 AND dependencia_id = $2
`, sesionID, claims.DependenciaID).Scan(
		&s.SesionID, &s.NombreSesion, &s.Estado, &s.IniciodaAt, &s.CerradaAt,
		&s.IniciodoPor, &s.TotalEnCatalogo, &s.TotalEscaneados,
		&s.Coincidencias, &s.UbicacionDiferente, &s.NoEnCatalogo, &s.Faltantes,
	)

	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusNotFound, models.Err("NO_ENCONTRADO", "Sesión no encontrada"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al obtener sesión"))
		return
	}

	writeJSON(w, http.StatusOK, models.OK(s))
}

func PausarSesion(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)
	sesionID := chi.URLParam(r, "id")

	tag, err := db.Pool.Exec(r.Context(), `
		UPDATE sesiones_escaneo
		SET estado = 'pausada'
		WHERE id = $1 AND dependencia_id = $2 AND estado = 'abierta'
	`, sesionID, claims.DependenciaID)

	if err != nil || tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusBadRequest, models.Err("NO_SE_PUEDE_PAUSAR", "La sesión no existe, no pertenece a tu dependencia, o ya no está abierta"))
		return
	}

	registrarAudit(r.Context(), claims.UsuarioID, sesionID, "sesiones_escaneo", "PAUSAR_SESION", nil, nil)
	writeJSON(w, http.StatusOK, models.OK(map[string]string{"estado": "pausada"}))
}

func ReanudarSesion(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)
	sesionID := chi.URLParam(r, "id")

	tag, err := db.Pool.Exec(r.Context(), `
		UPDATE sesiones_escaneo
		SET estado = 'abierta'
		WHERE id = $1 AND dependencia_id = $2 AND estado = 'pausada'
	`, sesionID, claims.DependenciaID)

	if err != nil || tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusBadRequest, models.Err("NO_SE_PUEDE_REANUDAR", "La sesión no existe o no está pausada"))
		return
	}

	registrarAudit(r.Context(), claims.UsuarioID, sesionID, "sesiones_escaneo", "REANUDAR_SESION", nil, nil)
	writeJSON(w, http.StatusOK, models.OK(map[string]string{"estado": "abierta"}))
}

func CerrarSesion(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)
	sesionID := chi.URLParam(r, "id")

	var nombreSesion string
	var totalEscaneados int
	err := db.Pool.QueryRow(r.Context(), `
		SELECT s.nombre_sesion, COUNT(e.id)
		FROM sesiones_escaneo s
		LEFT JOIN escaneos e ON e.sesion_id = s.id
		WHERE s.id = $1 AND s.dependencia_id = $2 AND s.estado != 'cerrada'
		GROUP BY s.nombre_sesion
	`, sesionID, claims.DependenciaID).Scan(&nombreSesion, &totalEscaneados)

	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusBadRequest, models.Err("NO_SE_PUEDE_CERRAR", "La sesión no existe, ya está cerrada, o no pertenece a tu dependencia"))
		return
	}

	now := time.Now().UTC()
	hashInput := fmt.Sprintf("%s|%s|%s|%d", sesionID, claims.UsuarioID, now.Format(time.RFC3339), totalEscaneados)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(hashInput)))

	_, err = db.Pool.Exec(r.Context(), `
		UPDATE sesiones_escaneo
		SET estado = 'cerrada',
		    cerrado_por = $1,
		    cerrada_at = $2,
		    hash_cierre = $3
		WHERE id = $4
	`, claims.UsuarioID, now, hash, sesionID)

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al cerrar sesión"))
		return
	}

	registrarAudit(r.Context(), claims.UsuarioID, sesionID, "sesiones_escaneo", "CERRAR_SESION", nil, map[string]interface{}{
		"hash_cierre":      hash,
		"total_escaneados": totalEscaneados,
		"cerrada_at":       now,
	})

	writeJSON(w, http.StatusOK, models.OK(map[string]interface{}{
		"sesion_id":        sesionID,
		"nombre_sesion":    nombreSesion,
		"estado":           "cerrada",
		"hash_cierre":      hash,
		"cerrada_at":       now,
		"total_escaneados": totalEscaneados,
	}))
}
