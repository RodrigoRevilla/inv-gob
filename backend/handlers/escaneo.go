package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"inv-gob/backend/db"
	mw "inv-gob/backend/middleware"
	"inv-gob/backend/models"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func RegistrarEscaneo(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)

	var req models.EscaneoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.Err("BODY_INVALIDO", "JSON inválido"))
		return
	}

	if req.SesionID == "" || req.NumeroInvLeido == "" {
		writeJSON(w, http.StatusBadRequest, models.Err("CAMPOS_REQUERIDOS", "sesion_id y numero_inv_leido son requeridos"))
		return
	}

	var sesionEstado string
	var versionID string
	err := db.Pool.QueryRow(r.Context(), `
		SELECT estado, version_id FROM sesiones_escaneo
		WHERE id = $1 AND dependencia_id = $2
	`, req.SesionID, claims.DependenciaID).Scan(&sesionEstado, &versionID)

	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusNotFound, models.Err("SESION_NO_ENCONTRADA", "Sesión no encontrada"))
		return
	}
	if sesionEstado != "abierta" {
		writeJSON(w, http.StatusBadRequest, models.Err("SESION_NO_ABIERTA",
			"No se puede escanear: la sesión está "+sesionEstado))
		return
	}

	var bien struct {
		ID                string
		Descripcion       string
		NumeroSerie       string
		Marca             string
		Modelo            string
		UbicacionEsperada string
		Resguardo         string
		Clasificacion     string
	}

	err = db.Pool.QueryRow(r.Context(), `
		SELECT id, descripcion,
		       COALESCE(numero_serie, ''),
		       COALESCE(marca, ''),
		       COALESCE(modelo, ''),
		       COALESCE(ubicacion_esperada, ''),
		       COALESCE(resguardo, ''),
		       COALESCE(clasificacion, '')
		FROM catalogo_bienes
		WHERE version_id = $1
		  AND numero_inventario = $2
		  AND estado = 'activo'
	`, versionID, req.NumeroInvLeido).Scan(
		&bien.ID, &bien.Descripcion, &bien.NumeroSerie,
		&bien.Marca, &bien.Modelo, &bien.UbicacionEsperada,
		&bien.Resguardo, &bien.Clasificacion,
	)

	var resultado string
	var bienIDParam interface{}
	var mensaje string

	if err == pgx.ErrNoRows {
		resultado = "no_en_catalogo"
		bienIDParam = nil
		mensaje = "Bien no registrado en el catálogo oficial"
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al buscar en catálogo"))
		return
	} else {
		bienIDParam = bien.ID

		if req.UbicacionEscaneada != "" &&
			bien.UbicacionEsperada != "" &&
			req.UbicacionEscaneada != bien.UbicacionEsperada {
			resultado = "encontrado"
			mensaje = "Bien encontrado pero en ubicación diferente a la registrada"
		} else {
			resultado = "coincide"
			mensaje = "Bien encontrado y verificado correctamente"
		}
	}

	var escaneoID string
	var escaneadoAt time.Time
	err = db.Pool.QueryRow(r.Context(), `
		INSERT INTO escaneos (sesion_id, bien_id, escaneado_por, resultado,
		                      numero_inv_leido, ubicacion_escaneada, observaciones)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, escaneado_at
	`, req.SesionID, bienIDParam, claims.UsuarioID, resultado,
		req.NumeroInvLeido, req.UbicacionEscaneada, req.Observaciones,
	).Scan(&escaneoID, &escaneadoAt)

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al registrar escaneo"))
		return
	}

	resp := models.EscaneoResponse{
		Resultado:          resultado,
		NumeroInvLeido:     req.NumeroInvLeido,
		Descripcion:        bien.Descripcion,
		NumeroSerie:        bien.NumeroSerie,
		Marca:              bien.Marca,
		Modelo:             bien.Modelo,
		UbicacionEsperada:  bien.UbicacionEsperada,
		Resguardo:          bien.Resguardo,
		UbicacionEscaneada: req.UbicacionEscaneada,
		Mensaje:            mensaje,
		EscaneadoAt:        escaneadoAt,
	}
	resp.ID.Scan(escaneoID)

	writeJSON(w, http.StatusCreated, models.OK(resp))
}

func ListarEscaneos(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)
	sesionID := r.URL.Query().Get("sesion_id")

	if sesionID == "" {
		writeJSON(w, http.StatusBadRequest, models.Err("SESION_REQUERIDA", "Parámetro sesion_id requerido"))
		return
	}

	var count int
	db.Pool.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM sesiones_escaneo
		WHERE id = $1 AND dependencia_id = $2
	`, sesionID, claims.DependenciaID).Scan(&count)

	if count == 0 {
		writeJSON(w, http.StatusNotFound, models.Err("SESION_NO_ENCONTRADA", "Sesión no encontrada"))
		return
	}

	rows, err := db.Pool.Query(r.Context(), `
		SELECT e.id, e.resultado, e.numero_inv_leido,
		       COALESCE(cb.descripcion, ''),
		       COALESCE(cb.numero_serie, ''),
		       COALESCE(cb.marca, ''),
		       COALESCE(cb.modelo, ''),
		       COALESCE(cb.ubicacion_esperada, ''),
		       COALESCE(cb.resguardo, ''),
		       COALESCE(e.ubicacion_escaneada, ''),
		       COALESCE(e.observaciones, ''),
		       u.nombre_completo,
		       e.escaneado_at
		FROM escaneos e
		JOIN usuarios u ON u.id = e.escaneado_por
		LEFT JOIN catalogo_bienes cb ON cb.id = e.bien_id
		WHERE e.sesion_id = $1
		ORDER BY e.escaneado_at DESC
		LIMIT 500
	`, sesionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al listar escaneos"))
		return
	}
	defer rows.Close()

	type EscaneoDetalle struct {
		ID                 string    `json:"id"`
		Resultado          string    `json:"resultado"`
		NumeroInvLeido     string    `json:"numero_inv_leido"`
		Descripcion        string    `json:"descripcion"`
		NumeroSerie        string    `json:"numero_serie"`
		Marca              string    `json:"marca"`
		Modelo             string    `json:"modelo"`
		UbicacionEsperada  string    `json:"ubicacion_esperada"`
		Resguardo          string    `json:"resguardo"`
		UbicacionEscaneada string    `json:"ubicacion_escaneada"`
		Observaciones      string    `json:"observaciones"`
		EscaneadoPor       string    `json:"escaneado_por"`
		EscaneadoAt        time.Time `json:"escaneado_at"`
	}

	var escaneos []EscaneoDetalle
	for rows.Next() {
		var e EscaneoDetalle
		if err := rows.Scan(
			&e.ID, &e.Resultado, &e.NumeroInvLeido,
			&e.Descripcion, &e.NumeroSerie, &e.Marca, &e.Modelo,
			&e.UbicacionEsperada, &e.Resguardo,
			&e.UbicacionEscaneada, &e.Observaciones,
			&e.EscaneadoPor, &e.EscaneadoAt,
		); err != nil {
			continue
		}
		escaneos = append(escaneos, e)
	}

	writeJSON(w, http.StatusOK, models.OK(escaneos))
}

func ListarFaltantes(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)
	sesionID := chi.URLParam(r, "id")

	rows, err := db.Pool.Query(r.Context(), `
		SELECT numero_inventario, numero_serie, descripcion,
		       marca, modelo, ubicacion_esperada, resguardo, clasificacion
		FROM v_faltantes_por_sesion
		WHERE sesion_id = $1
		  AND sesion_id IN (
		    SELECT id FROM sesiones_escaneo WHERE dependencia_id = $2
		  )
		ORDER BY numero_inventario
	`, sesionID, claims.DependenciaID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al obtener faltantes"))
		return
	}
	defer rows.Close()

	type Faltante struct {
		NumeroInventario  string `json:"numero_inventario"`
		NumeroSerie       string `json:"numero_serie"`
		Descripcion       string `json:"descripcion"`
		Marca             string `json:"marca"`
		Modelo            string `json:"modelo"`
		UbicacionEsperada string `json:"ubicacion_esperada"`
		Resguardo         string `json:"resguardo"`
		Clasificacion     string `json:"clasificacion"`
	}

	var faltantes []Faltante
	for rows.Next() {
		var f Faltante
		rows.Scan(
			&f.NumeroInventario, &f.NumeroSerie, &f.Descripcion,
			&f.Marca, &f.Modelo, &f.UbicacionEsperada,
			&f.Resguardo, &f.Clasificacion,
		)
		faltantes = append(faltantes, f)
	}

	writeJSON(w, http.StatusOK, models.OK(faltantes))
}