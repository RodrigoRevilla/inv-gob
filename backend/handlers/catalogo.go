package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"inv-gob/backend/db"
	mw "inv-gob/backend/middleware"
	"inv-gob/backend/models"
)

func ImportarCatalogo(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)

	var req models.ImportarCatalogoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.Err("BODY_INVALIDO", "JSON inválido"))
		return
	}

	if len(req.Bienes) == 0 {
		writeJSON(w, http.StatusBadRequest, models.Err("SIN_BIENES", "El catálogo no tiene bienes"))
		return
	}

	tx, err := db.Pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al iniciar transacción"))
		return
	}
	defer tx.Rollback(r.Context())

	var nextVersion int
	tx.QueryRow(r.Context(), `
		SELECT COALESCE(MAX(numero_version), 0) + 1
		FROM versiones_catalogo
		WHERE dependencia_id = $1
	`, claims.DependenciaID).Scan(&nextVersion)

	tx.Exec(r.Context(), `
		UPDATE versiones_catalogo SET activa = FALSE
		WHERE dependencia_id = $1 AND activa = TRUE
	`, claims.DependenciaID)

	var versionID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO versiones_catalogo
		  (dependencia_id, numero_version, importado_por, total_bienes, hash_archivo, nombre_archivo)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, claims.DependenciaID, nextVersion, claims.UsuarioID,
		len(req.Bienes), req.HashArchivo, req.NombreArchivo,
	).Scan(&versionID)

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al crear versión"))
		return
	}

	batch := make([][]interface{}, len(req.Bienes))
	for i, b := range req.Bienes {
		batch[i] = []interface{}{
			versionID, claims.DependenciaID,
			b.NumeroInventario, b.Descripcion,
			b.Clasificacion, b.UbicacionEsperada,
		}
	}

	_, err = tx.CopyFrom(r.Context(),
		[]string{"catalogo_bienes"},
		[]string{"version_id", "dependencia_id", "numero_inventario", "descripcion", "clasificacion", "ubicacion_esperada"},
		pgxCopyFromRows(batch),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al insertar bienes"))
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al confirmar transacción"))
		return
	}

	registrarAudit(r.Context(), claims.UsuarioID, "", "versiones_catalogo", "IMPORT_CATALOGO", nil, map[string]interface{}{
		"version_id":     versionID,
		"numero_version": nextVersion,
		"total_bienes":   len(req.Bienes),
		"nombre_archivo": req.NombreArchivo,
	})

	writeJSON(w, http.StatusCreated, models.OK(map[string]interface{}{
		"version_id":     versionID,
		"numero_version": nextVersion,
		"total_bienes":   len(req.Bienes),
	}))
}

func ListarCatalogo(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)

	rows, err := db.Pool.Query(r.Context(), `
		SELECT cb.id, cb.numero_inventario, cb.descripcion,
		       COALESCE(cb.clasificacion, ''), COALESCE(cb.ubicacion_esperada, ''), cb.estado
		FROM catalogo_bienes cb
		JOIN versiones_catalogo v ON v.id = cb.version_id
		WHERE cb.dependencia_id = $1 AND v.activa = TRUE
		ORDER BY cb.numero_inventario
		LIMIT 1000
	`, claims.DependenciaID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al listar catálogo"))
		return
	}
	defer rows.Close()

	type Bien struct {
		ID                string `json:"id"`
		NumeroInventario  string `json:"numero_inventario"`
		Descripcion       string `json:"descripcion"`
		Clasificacion     string `json:"clasificacion"`
		UbicacionEsperada string `json:"ubicacion_esperada"`
		Estado            string `json:"estado"`
	}

	var bienes []Bien
	for rows.Next() {
		var b Bien
		rows.Scan(&b.ID, &b.NumeroInventario, &b.Descripcion, &b.Clasificacion, &b.UbicacionEsperada, &b.Estado)
		bienes = append(bienes, b)
	}

	writeJSON(w, http.StatusOK, models.OK(bienes))
}

type pgxRows struct {
	rows [][]interface{}
	idx  int
}

func pgxCopyFromRows(rows [][]interface{}) *pgxRows {
	return &pgxRows{rows: rows}
}

func (r *pgxRows) Next() bool {
	r.idx++
	return r.idx <= len(r.rows)
}

func (r *pgxRows) Values() ([]interface{}, error) {
	return r.rows[r.idx-1], nil
}

func (r *pgxRows) Err() error { return nil }

func registrarAudit(ctx context.Context, usuarioID, sesionID, tabla, operacion string, antes, despues interface{}) {
	antesJSON, _ := json.Marshal(antes)
	despuesJSON, _ := json.Marshal(despues)

	var sesionParam interface{}
	if sesionID != "" {
		sesionParam = sesionID
	}

	db.Pool.Exec(ctx, `
		INSERT INTO audit_log (usuario_id, sesion_id, tabla_afectada, operacion, datos_anteriores, datos_nuevos)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, usuarioID, sesionParam, tabla, operacion, antesJSON, despuesJSON)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func ListarAuditLog(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)

	rows, err := db.Pool.Query(r.Context(), `
SELECT 
    a.id,
    a.operacion,
    a.tabla_afectada,
    a.datos_nuevos,
    a.registrado_at,
    u.nombre_completo,
    u.usuario,
    u.rol
FROM audit_log a
JOIN usuarios u ON u.id = a.usuario_id
WHERE u.dependencia_id = $1
ORDER BY a.registrado_at DESC
LIMIT 200
	`, claims.DependenciaID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al listar log"))
		return
	}
	defer rows.Close()

	type EntradaLog struct {
		ID             string          `json:"id"`
		Operacion      string          `json:"operacion"`
		TablaAfectada  string          `json:"tabla_afectada"`
		DatosNuevos    json.RawMessage `json:"datos_nuevos"`
		CreatedAt      string          `json:"created_at"`
		NombreCompleto string          `json:"nombre_completo"`
		Usuario        string          `json:"usuario"`
		Rol            string          `json:"rol"`
	}

	var entradas []EntradaLog
	for rows.Next() {
		var e EntradaLog
		var datosNuevos []byte
		var createdAt interface{}
		if err := rows.Scan(
			&e.ID, &e.Operacion, &e.TablaAfectada,
			&datosNuevos, &createdAt,
			&e.NombreCompleto, &e.Usuario, &e.Rol,
		); err != nil {
			continue
		}
		if datosNuevos != nil {
			e.DatosNuevos = json.RawMessage(datosNuevos)
		} else {
			e.DatosNuevos = json.RawMessage(`{}`)
		}
		if t, ok := createdAt.(interface{ String() string }); ok {
			e.CreatedAt = t.String()
		} else {
			e.CreatedAt = fmt.Sprintf("%v", createdAt)
		}
		entradas = append(entradas, e)
	}

	if entradas == nil {
		entradas = []EntradaLog{}
	}

	writeJSON(w, http.StatusOK, models.OK(entradas))
}
