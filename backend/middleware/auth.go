package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"inv-gob/backend/models"
)

type contextKey string

const ClaimsKey contextKey = "claims"

var jwtSecret []byte

func SetJWTSecret(secret string) {
	jwtSecret = []byte(secret)
}

func GenerarToken(usuarioID, dependenciaID, rol string) (string, error) {
	claims := jwt.MapClaims{
		"usuario_id":     usuarioID,
		"dependencia_id": dependenciaID,
		"rol":            rol,
		"exp":            time.Now().Add(8 * time.Hour).Unix(),
		"iat":            time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			writeErr(w, http.StatusUnauthorized, "AUTH_REQUERIDO", "Token no proporcionado")
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			writeErr(w, http.StatusUnauthorized, "TOKEN_INVALIDO", "Token inválido o expirado")
			return
		}

		mapClaims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "TOKEN_INVALIDO", "Claims inválidos")
			return
		}

		claims := &models.Claims{
			UsuarioID:     mapClaims["usuario_id"].(string),
			DependenciaID: mapClaims["dependencia_id"].(string),
			Rol:           mapClaims["rol"].(string),
		}

		ctx := context.WithValue(r.Context(), ClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequiereRol(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r)
			if claims == nil {
				writeErr(w, http.StatusUnauthorized, "AUTH_REQUERIDO", "No autenticado")
				return
			}

			for _, rol := range roles {
				if claims.Rol == rol {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeErr(w, http.StatusForbidden, "SIN_PERMISO", "Tu rol no tiene acceso a este recurso")
		})
	}
}

func GetClaims(r *http.Request) *models.Claims {
	claims, _ := r.Context().Value(ClaimsKey).(*models.Claims)
	return claims
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(models.Err(code, msg))
}