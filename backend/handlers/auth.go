package handlers

import (
	"encoding/json"
	"net/http"

	"inv-gob/backend/db"
	mw "inv-gob/backend/middleware"
	"inv-gob/backend/models"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

func Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.Err("BODY_INVALIDO", "JSON inválido"))
		return
	}

	if req.Usuario == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, models.Err("CAMPOS_REQUERIDOS", "Usuario y contraseña requeridos"))
		return
	}

	var u models.Usuario
	var passwordHash string

	err := db.Pool.QueryRow(r.Context(), `
		SELECT u.id, u.dependencia_id, u.nombre_completo, u.usuario,
		       u.password_hash, u.rol, u.activo, u.created_at
		FROM usuarios u
		WHERE u.usuario = $1 AND u.activo = TRUE
	`, req.Usuario).Scan(
		&u.ID, &u.DependenciaID, &u.NombreCompleto, &u.Usuario,
		&passwordHash, &u.Rol, &u.Activo, &u.CreatedAt,
	)

	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusUnauthorized, models.Err("CREDENCIALES_INVALIDAS", "Usuario o contraseña incorrectos"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error interno"))
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, models.Err("CREDENCIALES_INVALIDAS", "Usuario o contraseña incorrectos"))
		return
	}

	depID := u.DependenciaID.String()
	uID := u.ID.String()

	token, err := mw.GenerarToken(uID, depID, u.Rol)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("TOKEN_ERROR", "No se pudo generar el token"))
		return
	}

	registrarAudit(r.Context(), uID, "", "usuarios", "LOGIN", nil, map[string]string{
		"usuario": u.Usuario,
		"rol":     u.Rol,
	})

	writeJSON(w, http.StatusOK, models.OK(models.LoginResponse{
		Token:   token,
		Usuario: &u,
	}))
}

func CrearUsuario(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)

	var req struct {
		NombreCompleto string `json:"nombre_completo"`
		Usuario        string `json:"usuario"`
		Password       string `json:"password"`
		Rol            string `json:"rol"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.Err("BODY_INVALIDO", "JSON inválido"))
		return
	}

	if req.NombreCompleto == "" || req.Usuario == "" || req.Password == "" || req.Rol == "" {
		writeJSON(w, http.StatusBadRequest, models.Err("CAMPOS_REQUERIDOS", "Todos los campos son requeridos"))
		return
	}

	rolesValidos := map[string]bool{"escaner": true, "supervisor": true, "auditor": true, "admin": true}
	if !rolesValidos[req.Rol] {
		writeJSON(w, http.StatusBadRequest, models.Err("ROL_INVALIDO", "Rol no válido"))
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("HASH_ERROR", "Error al procesar contraseña"))
		return
	}

	var id string
	err = db.Pool.QueryRow(r.Context(), `
		INSERT INTO usuarios (dependencia_id, nombre_completo, usuario, password_hash, rol)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, claims.DependenciaID, req.NombreCompleto, req.Usuario, string(hash), req.Rol).Scan(&id)

	if err != nil {
		writeJSON(w, http.StatusConflict, models.Err("USUARIO_DUPLICADO", "El nombre de usuario ya existe en esta dependencia"))
		return
	}

	registrarAudit(r.Context(), claims.UsuarioID, "", "usuarios", "CREAR_USUARIO", nil, map[string]string{
		"usuario_creado": req.Usuario,
		"rol":            req.Rol,
	})

	writeJSON(w, http.StatusCreated, models.OK(map[string]string{"id": id}))
}

func ListarUsuarios(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)

	rows, err := db.Pool.Query(r.Context(), `
		SELECT id, dependencia_id, nombre_completo, usuario, rol, activo, created_at
		FROM usuarios
		WHERE dependencia_id = $1
		ORDER BY nombre_completo
	`, claims.DependenciaID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al listar usuarios"))
		return
	}
	defer rows.Close()

	var usuarios []models.Usuario
	for rows.Next() {
		var u models.Usuario
		if err := rows.Scan(&u.ID, &u.DependenciaID, &u.NombreCompleto, &u.Usuario, &u.Rol, &u.Activo, &u.CreatedAt); err != nil {
			continue
		}
		usuarios = append(usuarios, u)
	}

	writeJSON(w, http.StatusOK, models.OK(usuarios))
}

func ToggleUsuario(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)
	id := r.PathValue("id")

	if id == claims.UsuarioID {
		writeJSON(w, http.StatusBadRequest, models.Err("NO_PERMITIDO", "No puedes desactivar tu propia cuenta"))
		return
	}

	var activo bool
	err := db.Pool.QueryRow(r.Context(), `
		SELECT activo FROM usuarios
		WHERE id = $1 AND dependencia_id = $2
	`, id, claims.DependenciaID).Scan(&activo)

	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusNotFound, models.Err("NO_ENCONTRADO", "Usuario no encontrado"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error interno"))
		return
	}

	nuevoEstado := !activo
	_, err = db.Pool.Exec(r.Context(), `
		UPDATE usuarios SET activo = $1 WHERE id = $2
	`, nuevoEstado, id)

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al actualizar usuario"))
		return
	}

	estado := "activado"
	if !nuevoEstado {
		estado = "desactivado"
	}

	registrarAudit(r.Context(), claims.UsuarioID, "", "usuarios", "TOGGLE_USUARIO", nil, map[string]interface{}{
		"usuario_id":   id,
		"nuevo_estado": nuevoEstado,
		"accion":       estado,
	})

	writeJSON(w, http.StatusOK, models.OK(map[string]any{"activo": nuevoEstado, "mensaje": "Usuario " + estado}))
}

func ResetPassword(w http.ResponseWriter, r *http.Request) {
	claims := mw.GetClaims(r)
	id := r.PathValue("id")

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.Err("BODY_INVALIDO", "JSON inválido"))
		return
	}
	if len(req.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, models.Err("PASSWORD_INVALIDO", "La contraseña debe tener al menos 6 caracteres"))
		return
	}

	var exists bool
	err := db.Pool.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM usuarios WHERE id = $1 AND dependencia_id = $2)
	`, id, claims.DependenciaID).Scan(&exists)

	if err != nil || !exists {
		writeJSON(w, http.StatusNotFound, models.Err("NO_ENCONTRADO", "Usuario no encontrado"))
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("HASH_ERROR", "Error al procesar contraseña"))
		return
	}

	_, err = db.Pool.Exec(r.Context(), `
		UPDATE usuarios SET password_hash = $1 WHERE id = $2
	`, string(hash), id)

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.Err("DB_ERROR", "Error al actualizar contraseña"))
		return
	}

	registrarAudit(r.Context(), claims.UsuarioID, "", "usuarios", "RESET_PASSWORD", nil, map[string]string{
		"usuario_id": id,
	})

	writeJSON(w, http.StatusOK, models.OK(map[string]string{"mensaje": "Contraseña actualizada correctamente"}))
}
