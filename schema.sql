--  SISTEMA DE ESCANEO DE INVENTARIO GUBERNAMENTAL
--  Schema PostgreSQL — v1.0
--  Principios: inmutabilidad, trazabilidad, no repudio

-- Extensiones necesarias
CREATE EXTENSION IF NOT EXISTS "pgcrypto";   
CREATE EXTENSION IF NOT EXISTS "btree_gist"; 

--  TIPOS ENUMERADOS
--  Valores fijos a nivel de DB, no sólo a nivel de aplicación

CREATE TYPE rol_usuario AS ENUM (
  'escaner',      -- sólo puede escanear, sin acceso a reportes
  'supervisor',   -- ve resultados en tiempo real, puede pausar sesión
  'auditor',      -- lectura total, no puede modificar nada
  'admin'         -- gestiona usuarios y catálogo, no puede alterar audit_log
);

CREATE TYPE estado_bien AS ENUM (
  'activo',
  'baja',
  'extraviado',
  'en_proceso_baja'
);

CREATE TYPE estado_sesion AS ENUM (
  'abierta',
  'pausada',
  'cerrada'   -- estado final, no reversible
);

CREATE TYPE resultado_escaneo AS ENUM (
  'coincide',          -- encontrado y en ubicación correcta
  'encontrado',        -- encontrado pero en ubicación diferente
  'no_en_catalogo'    -- el código escaneado no existe en el catálogo
);

--  TABLA: dependencias
--  Raíz del sistema. Cada dependencia gubernamental es aislada.

CREATE TABLE dependencias (
  id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  clave_dependencia VARCHAR(20) NOT NULL UNIQUE,
  nombre            TEXT        NOT NULL,
  responsable       TEXT        NOT NULL,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

--  TABLA: usuarios


CREATE TABLE usuarios (
  id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  dependencia_id   UUID        NOT NULL REFERENCES dependencias(id),
  nombre_completo  TEXT        NOT NULL,
  usuario          VARCHAR(60) NOT NULL,
  password_hash    TEXT        NOT NULL,   
  rol              rol_usuario NOT NULL,
  activo           BOOLEAN     NOT NULL DEFAULT TRUE,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT uq_usuario_por_dependencia UNIQUE (dependencia_id, usuario)
);

CREATE INDEX idx_usuarios_dependencia ON usuarios(dependencia_id);
CREATE INDEX idx_usuarios_activo      ON usuarios(activo) WHERE activo = TRUE;

--  TABLA: versiones_catalogo
--  Cada importación del catálogo oficial crea una nueva versión.
--  Las versiones anteriores se conservan para auditoría histórica.

CREATE TABLE versiones_catalogo (
  id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  dependencia_id UUID        NOT NULL REFERENCES dependencias(id),
  numero_version INTEGER     NOT NULL,
  activa         BOOLEAN     NOT NULL DEFAULT TRUE,
  importado_por  UUID        NOT NULL REFERENCES usuarios(id),
  total_bienes   INTEGER     NOT NULL DEFAULT 0,
  hash_archivo   TEXT        NOT NULL,  
  nombre_archivo TEXT        NOT NULL,
  importado_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT uq_version_por_dependencia UNIQUE (dependencia_id, numero_version)
);

-- Sólo una versión activa por dependencia
CREATE UNIQUE INDEX idx_una_version_activa
  ON versiones_catalogo(dependencia_id)
  WHERE activa = TRUE;

--  TABLA: catalogo_bienes
--  Catálogo oficial importado. NUNCA se edita un registro;
--  si hay corrección se crea una nueva versión del catálogo.

CREATE TABLE catalogo_bienes (
  id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  version_id        UUID        NOT NULL REFERENCES versiones_catalogo(id),
  dependencia_id    UUID        NOT NULL REFERENCES dependencias(id),
  numero_inventario VARCHAR(80) NOT NULL,
  descripcion       TEXT        NOT NULL,
  clasificacion     VARCHAR(60),
  ubicacion_esperada TEXT,
  estado            estado_bien NOT NULL DEFAULT 'activo',
  metadatos         JSONB,      
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT uq_numero_inv_por_version
    UNIQUE (version_id, numero_inventario)
);

CREATE INDEX idx_bienes_version       ON catalogo_bienes(version_id);
CREATE INDEX idx_bienes_dependencia   ON catalogo_bienes(dependencia_id);
CREATE INDEX idx_bienes_numero_inv    ON catalogo_bienes(numero_inventario);
CREATE INDEX idx_bienes_estado        ON catalogo_bienes(estado);

-- Bloqueo de modificaciones al catálogo ya importado
CREATE RULE no_update_catalogo AS
  ON UPDATE TO catalogo_bienes DO INSTEAD NOTHING;

CREATE RULE no_delete_catalogo AS
  ON DELETE TO catalogo_bienes DO INSTEAD NOTHING;

--  TABLA: sesiones_escaneo
--  Una sesión = un acto formal de inventario (puede ser parcial
--  por área, edificio, etc.). Una vez cerrada no se reabre.

CREATE TABLE sesiones_escaneo (
  id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
  dependencia_id  UUID          NOT NULL REFERENCES dependencias(id),
  version_id      UUID          NOT NULL REFERENCES versiones_catalogo(id),
  iniciado_por    UUID          NOT NULL REFERENCES usuarios(id),
  cerrado_por     UUID          REFERENCES usuarios(id),
  nombre_sesion   TEXT          NOT NULL,
  descripcion     TEXT,
  area_cubierta   TEXT,        
  estado          estado_sesion NOT NULL DEFAULT 'abierta',
  hash_cierre     TEXT,        
  iniciada_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
  cerrada_at      TIMESTAMPTZ,

  CONSTRAINT ck_cerrada_tiene_hash
    CHECK (estado != 'cerrada' OR hash_cierre IS NOT NULL),
  CONSTRAINT ck_cerrada_tiene_usuario
    CHECK (estado != 'cerrada' OR cerrado_por IS NOT NULL),
  CONSTRAINT ck_cerrada_tiene_fecha
    CHECK (estado != 'cerrada' OR cerrada_at IS NOT NULL)
);

-- Una sesión cerrada no puede volver a ningún otro estado
CREATE OR REPLACE FUNCTION fn_proteger_sesion_cerrada()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.estado = 'cerrada' THEN
    RAISE EXCEPTION 'Una sesión cerrada no puede modificarse (id: %)', OLD.id;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER trg_proteger_sesion_cerrada
  BEFORE UPDATE ON sesiones_escaneo
  FOR EACH ROW EXECUTE FUNCTION fn_proteger_sesion_cerrada();

CREATE INDEX idx_sesiones_dependencia ON sesiones_escaneo(dependencia_id);
CREATE INDEX idx_sesiones_estado      ON sesiones_escaneo(estado);
CREATE INDEX idx_sesiones_iniciada    ON sesiones_escaneo(iniciada_at DESC);

--  TABLA: escaneos
--  Registro de cada lectura de código. Inmutable post-inserción.
--  No hay UPDATE ni DELETE — si hubo error, se registra uno nuevo
--  con resultado 'corregido' y referencia al escaneo original.

CREATE TABLE escaneos (
  id                  UUID              PRIMARY KEY DEFAULT gen_random_uuid(),
  sesion_id           UUID              NOT NULL REFERENCES sesiones_escaneo(id),
  bien_id             UUID              REFERENCES catalogo_bienes(id), 
  escaneado_por       UUID              NOT NULL REFERENCES usuarios(id),
  resultado           resultado_escaneo NOT NULL,
  numero_inv_leido    VARCHAR(80)       NOT NULL, 
  ubicacion_escaneada TEXT,
  observaciones       TEXT,
  escaneado_at        TIMESTAMPTZ       NOT NULL DEFAULT now()
);

-- No se puede escanear en una sesión cerrada
CREATE OR REPLACE FUNCTION fn_validar_sesion_abierta()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
  v_estado estado_sesion;
BEGIN
  SELECT estado INTO v_estado
  FROM sesiones_escaneo
  WHERE id = NEW.sesion_id;

  IF v_estado != 'abierta' THEN
    RAISE EXCEPTION 'No se puede escanear en una sesión % (id: %)', v_estado, NEW.sesion_id;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER trg_validar_sesion_abierta
  BEFORE INSERT ON escaneos
  FOR EACH ROW EXECUTE FUNCTION fn_validar_sesion_abierta();

-- Bloqueo absoluto de modificaciones
CREATE RULE no_update_escaneos AS
  ON UPDATE TO escaneos DO INSTEAD NOTHING;

CREATE RULE no_delete_escaneos AS
  ON DELETE TO escaneos DO INSTEAD NOTHING;

CREATE INDEX idx_escaneos_sesion     ON escaneos(sesion_id);
CREATE INDEX idx_escaneos_bien       ON escaneos(bien_id);
CREATE INDEX idx_escaneos_resultado  ON escaneos(resultado);
CREATE INDEX idx_escaneos_at         ON escaneos(escaneado_at DESC);
CREATE INDEX idx_escaneos_numero_inv ON escaneos(numero_inv_leido);

--  TABLA: audit_log
--  INSERT ONLY. No UPDATE, no DELETE. Jamás.
--  Registra toda operación relevante con contexto completo.

CREATE TABLE audit_log (
  id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  usuario_id      UUID        REFERENCES usuarios(id),  
  sesion_id       UUID        REFERENCES sesiones_escaneo(id),
  tabla_afectada  TEXT        NOT NULL,
  operacion       TEXT        NOT NULL, 
  datos_anteriores JSONB,
  datos_nuevos     JSONB,
  ip_origen       INET,
  user_agent      TEXT,
  registrado_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Política de solo inserción
CREATE RULE no_update_audit AS
  ON UPDATE TO audit_log DO INSTEAD NOTHING;

CREATE RULE no_delete_audit AS
  ON DELETE TO audit_log DO INSTEAD NOTHING;

-- Seguridad adicional a nivel de rol de DB (ejecutar como superuser)
-- REVOKE UPDATE, DELETE ON audit_log FROM app_role;

CREATE INDEX idx_audit_usuario    ON audit_log(usuario_id);
CREATE INDEX idx_audit_sesion     ON audit_log(sesion_id);
CREATE INDEX idx_audit_operacion  ON audit_log(operacion);
CREATE INDEX idx_audit_registrado ON audit_log(registrado_at DESC);

--  VISTAS ÚTILES PARA REPORTES

-- Resumen de una sesión: cuántos bienes tiene el catálogo,
-- cuántos se escanearon, faltantes y sobrantes
CREATE VIEW v_resumen_sesion AS
SELECT
  s.id                                        AS sesion_id,
  s.nombre_sesion,
  s.estado,
  s.iniciada_at,
  s.cerrada_at,
  u_ini.nombre_completo                       AS iniciado_por,
  u_cie.nombre_completo                       AS cerrado_por,
  v.numero_version                            AS version_catalogo,
  COUNT(DISTINCT cb.id)                       AS total_en_catalogo,
  COUNT(DISTINCT e.bien_id)
    FILTER (WHERE e.resultado IN ('coincide','encontrado'))
                                              AS total_escaneados,
  COUNT(DISTINCT e.id)
    FILTER (WHERE e.resultado = 'coincide')   AS coincidencias,
  COUNT(DISTINCT e.id)
    FILTER (WHERE e.resultado = 'encontrado') AS ubicacion_diferente,
  COUNT(DISTINCT e.id)
    FILTER (WHERE e.resultado = 'no_en_catalogo')
                                              AS no_en_catalogo,
  COUNT(DISTINCT cb.id) -
    COUNT(DISTINCT e.bien_id)
    FILTER (WHERE e.resultado IN ('coincide','encontrado'))
                                              AS faltantes
FROM sesiones_escaneo s
JOIN versiones_catalogo v    ON v.id = s.version_id
JOIN catalogo_bienes cb      ON cb.version_id = v.id AND cb.estado = 'activo'
JOIN usuarios u_ini          ON u_ini.id = s.iniciado_por
LEFT JOIN usuarios u_cie     ON u_cie.id = s.cerrado_por
LEFT JOIN escaneos e         ON e.sesion_id = s.id
GROUP BY s.id, s.nombre_sesion, s.estado, s.iniciada_at, s.cerrada_at,
         u_ini.nombre_completo, u_cie.nombre_completo, v.numero_version;

-- Bienes faltantes en una sesión (los que están en catálogo pero no fueron escaneados)
CREATE VIEW v_faltantes_por_sesion AS
SELECT
  s.id           AS sesion_id,
  s.nombre_sesion,
  cb.numero_inventario,
  cb.descripcion,
  cb.ubicacion_esperada,
  cb.clasificacion
FROM sesiones_escaneo s
JOIN versiones_catalogo v ON v.id = s.version_id
JOIN catalogo_bienes cb   ON cb.version_id = v.id AND cb.estado = 'activo'
WHERE NOT EXISTS (
  SELECT 1 FROM escaneos e
  WHERE e.sesion_id = s.id
    AND e.bien_id = cb.id
    AND e.resultado IN ('coincide', 'encontrado')
);

--  ROL DE APLICACIÓN (mínimos privilegios)
--  Crear el usuario de la app con estos permisos solamente
-- CREATE ROLE inv_app LOGIN PASSWORD '...';
-- GRANT SELECT, INSERT ON ALL TABLES IN SCHEMA public TO inv_app;
-- GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO inv_app;
-- REVOKE UPDATE, DELETE ON catalogo_bienes FROM inv_app;
-- REVOKE UPDATE, DELETE ON escaneos FROM inv_app;
-- REVOKE UPDATE, DELETE ON audit_log FROM inv_app;
-- GRANT UPDATE ON sesiones_escaneo TO inv_app;   -- sólo para cambiar estado
-- GRANT UPDATE ON usuarios TO inv_app;            -- sólo para cambiar activo/password