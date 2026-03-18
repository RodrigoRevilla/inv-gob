package models

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type Response struct {
	Ok    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error *APIError   `json:"error,omitempty"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func OK(data interface{}) Response {
	return Response{Ok: true, Data: data}
}

func Err(code, message string) Response {
	return Response{Ok: false, Error: &APIError{Code: code, Message: message}}
}

type Usuario struct {
	ID             pgtype.UUID `json:"id"`
	DependenciaID  pgtype.UUID `json:"dependencia_id"`
	NombreCompleto string      `json:"nombre_completo"`
	Usuario        string      `json:"usuario"`
	Rol            string      `json:"rol"`
	Activo         bool        `json:"activo"`
	CreatedAt      time.Time   `json:"created_at"`
}

type LoginRequest struct {
	Usuario  string `json:"usuario"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token   string   `json:"token"`
	Usuario *Usuario `json:"usuario"`
}

type CatalogoBien struct {
	ID                pgtype.UUID `json:"id"`
	VersionID         pgtype.UUID `json:"version_id"`
	DependenciaID     pgtype.UUID `json:"dependencia_id"`
	NumeroInventario  string      `json:"numero_inventario"`
	Descripcion       string      `json:"descripcion"`
	Clasificacion     string      `json:"clasificacion"`
	UbicacionEsperada string      `json:"ubicacion_esperada"`
	Estado            string      `json:"estado"`
	CreatedAt         time.Time   `json:"created_at"`
}

type ImportarCatalogoRequest struct {
	NombreArchivo string         `json:"nombre_archivo"`
	HashArchivo   string         `json:"hash_archivo"`
	Bienes        []BienImportar `json:"bienes"`
}

type BienImportar struct {
	NumeroInventario  string `json:"numero_inventario"`
	Descripcion       string `json:"descripcion"`
	Clasificacion     string `json:"clasificacion"`
	UbicacionEsperada string `json:"ubicacion_esperada"`
}

type SesionEscaneo struct {
	ID            pgtype.UUID `json:"id"`
	DependenciaID pgtype.UUID `json:"dependencia_id"`
	IniciodoPor   pgtype.UUID `json:"iniciado_por"`
	NombreSesion  string      `json:"nombre_sesion"`
	Descripcion   string      `json:"descripcion"`
	AreaCubierta  string      `json:"area_cubierta"`
	Estado        string      `json:"estado"`
	HashCierre    string      `json:"hash_cierre,omitempty"`
	IniciodaAt    time.Time   `json:"iniciada_at"`
	CerradaAt     *time.Time  `json:"cerrada_at,omitempty"`
}

type CrearSesionRequest struct {
	NombreSesion string `json:"nombre_sesion"`
	Descripcion  string `json:"descripcion"`
	AreaCubierta string `json:"area_cubierta"`
}

type ResumenSesion struct {
	SesionID           pgtype.UUID `json:"sesion_id"`
	NombreSesion       string      `json:"nombre_sesion"`
	Estado             string      `json:"estado"`
	IniciodaAt         time.Time   `json:"iniciada_at"`
	CerradaAt          *time.Time  `json:"cerrada_at"`
	IniciodoPor        string      `json:"iniciado_por"`
	TotalEnCatalogo    int         `json:"total_en_catalogo"`
	TotalEscaneados    int         `json:"total_escaneados"`
	Coincidencias      int         `json:"coincidencias"`
	UbicacionDiferente int         `json:"ubicacion_diferente"`
	NoEnCatalogo       int         `json:"no_en_catalogo"`
	Faltantes          int         `json:"faltantes"`
}

type EscaneoRequest struct {
	SesionID           string `json:"sesion_id"`
	NumeroInvLeido     string `json:"numero_inv_leido"`
	UbicacionEscaneada string `json:"ubicacion_escaneada"`
	Observaciones      string `json:"observaciones"`
}

type EscaneoResponse struct {
	ID                 pgtype.UUID `json:"id"`
	Resultado          string      `json:"resultado"`
	NumeroInvLeido     string      `json:"numero_inv_leido"`
	Descripcion        string      `json:"descripcion,omitempty"`
	UbicacionEsperada  string      `json:"ubicacion_esperada,omitempty"`
	UbicacionEscaneada string      `json:"ubicacion_escaneada,omitempty"`
	Mensaje            string      `json:"mensaje"`
	EscaneadoAt        time.Time   `json:"escaneado_at"`
}

type Claims struct {
	UsuarioID     string `json:"usuario_id"`
	DependenciaID string `json:"dependencia_id"`
	Rol           string `json:"rol"`
}
