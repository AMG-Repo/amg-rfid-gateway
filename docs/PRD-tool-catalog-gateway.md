# PRD: Catálogo de Herramientas en Gateway Web

**Proyecto**: amg-rfid-gateway  
**Fecha**: 2026-05-08  
**Estado**: Borrador — Listo para revisión  
**Relacionado**: VPS `erp-demo-kanban-admin` (PR #2, #4)

---

## 1. Descripción General

### 1.1 Problema

La web UI del gateway (`/`) solo muestra:
- Tags detectados en tiempo real (EPC, RSSI, antena)
- Confirmaciones de entrada/salida
- Estado de conexión con VPS

**No hay forma de ver el catálogo de herramientas** desde la Raspberry Pi. El operador en planta no puede verificar:
- Qué herramientas existen en el sistema
- En qué ubicación está cada una
- Su estado (disponible/en uso/mantenimiento)
- Información KANBAN (zona destino, estándar pack, máximo producción)

### 1.2 Solución

Agregar una nueva sección/pantalla en la web UI del gateway que muestre el **catálogo de herramientas** sincronizado desde el VPS, con la misma información que muestra el frontend VPS en `/private/tool`.

### 1.3 Objetivos

| Objetivo | Métrica |
|----------|---------|
| Ver catálogo de herramientas en gateway | Tabla/cards con todas las herramientas sincronizadas |
| Información por herramienta | SKU, nombre, estado, ubicación, último visto |
| Datos KANBAN visibles | Zona destino, estándar pack, máximo producción |
| Auto-refresh | Datos se actualizan cada 30 segundos desde VPS |
| Offline-first | Si el VPS no está disponible, mostrar últimos datos sincronizados |

---

## 2. Requisitos

### REQ-001: Endpoint `GET /api/tools`

El gateway DEBE exponer un endpoint que devuelva las herramientas缓存adas localmente:

```
GET /api/tools
Response:
{
  "status": "ok",
  "count": 18,
  "tools": [
    {
      "id": 1,
      "sku": "EX010BLA",
      "name": "EX010BLA",
      "description": "...",
      "uii": "E2001234...",
      "status": "available",
      "location": "Almacen General",
      "last_seen": "2026-05-08T10:30:00Z"
    }
  ],
  "timestamp": "2026-05-08T10:30:00Z"
}
```

**Fuente**: `localstore.GetTools()` (ya existe en SQLite local)

### REQ-002: Endpoint `GET /api/kanbans`

El gateway DEBE exponer los datos KANBAN sincronizados desde el VPS:

```
GET /api/kanbans
Response:
{
  "status": "ok",
  "count": 5,
  "kanbans": [
    {
      "code": "EXC430WF",
      "zone": "G",
      "description": "Extrusión Primary Seal FR",
      "standard_pack": 150,
      "max_production": 240
    }
  ],
  "timestamp": "2026-05-08T10:30:00Z"
}
```

**Nota**: Requiere sincronización desde VPS. Ver REQ-005.

### REQ-003: Pantalla "Herramientas" en Web UI

Nueva pestaña/sección en la web UI del gateway:

```
┌─────────────────────────────────────────────┐
│  AMG RFID Gateway                    [🟢]   │
├─────────────────────────────────────────────┤
│  [Tags] [Herramientas] [KANBAN] [Estado]   │
├─────────────────────────────────────────────┤
│                                             │
│  Herramientas (18)          [Actualizar]    │
│                                             │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐      │
│  │ EX010BLA│ │ EX150BLA│ │ EXC430SF│      │
│  │ Disponible│ │ En uso  │ │ En uso  │      │
│  │ Almacén A│ │ Almacén B│ │ Inspección│    │
│  └─────────┘ └─────────┘ └─────────┘      │
│                                             │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐      │
│  │ EXC430CP│ │ EX3W0AF │ │ HERR-1  │      │
│  │ Disponible│ │ En uso  │ │ Disponible│    │
│  │ Almacén A│ │ Almacén B│ │ Almacén A│    │
│  └─────────┘ └─────────┘ └─────────┘      │
│                                             │
└─────────────────────────────────────────────┘
```

### REQ-004: Tarjeta de herramienta

Cada tarjeta DEBE mostrar:
- **SKU** (código de la herramienta)
- **Nombre** (descripción legible)
- **Estado**: badge de color (verde=disponible, amarillo=en uso, rojo=mantenimiento)
- **Ubicación**: última ubicación conocida
- **Último visto**: timestamp de última detección

### REQ-005: Sincronización desde VPS

El gateway DEBE sincronizar datos del VPS periódicamente:

- **Frecuencia**: cada 30 segundos (configurable)
- **Endpoint VPS**: `GET /api/tools` (con JWT auth)
- **Almacenamiento**: SQLite local (`localstore`)
- **Offline**: si el VPS no responde, mantener últimos datos
- **KANBAN**: sincronizar también `GET /api/kanbans` desde VPS

### REQ-006: Navegación por pestañas

La web UI DEBE tener navegación por pestañas:
- **Tags** (vista actual — detección en tiempo real)
- **Herramientas** (catálogo sincronizado)
- **KANBAN** (métricas de producción — opcional, futuro)
- **Estado** (conexión VPS, cola de confirmaciones)

---

## 3. Modelo de Datos

### Tool (localstore — ya existe)

```go
type Tool struct {
    ID           int64     `json:"id"`
    CompanyID    string    `json:"company_id"`
    SKU          string    `json:"sku"`
    Name         string    `json:"name"`
    Description  string    `json:"description"`
    UII          string    `json:"uii"`
    Status       string    `json:"status"`
    Location     string    `json:"location"`
    LastSyncedAt time.Time `json:"last_synced_at"`
}
```

### Kanban (nuevo — sincronizado desde VPS)

```go
type Kanban struct {
    ID            int64     `json:"id"`
    CompanyID     string    `json:"company_id"`
    Code          string    `json:"code"`
    Zone          string    `json:"zone"`
    Description   string    `json:"description"`
    StandardPack  int       `json:"standard_pack"`
    MaxProduction int       `json:"max_production"`
    LastSyncedAt  time.Time `json:"last_synced_at"`
}
```

---

## 4. Fuera de Alcance

- Edición de herramientas desde el gateway (solo lectura)
- CRUD de KANBAN desde el gateway (solo lectura)
- Filtros/búsqueda avanzada (futuro)
- Gráficos/charts (futuro)
- Autenticación en la web UI del gateway (es red local)

---

## 5. Riesgos

| Riesgo | Impacto | Mitigación |
|--------|---------|------------|
| VPS caído, gateway sin datos nuevos | Medio | Offline-first: mostrar últimos datos sincronizados |
| Latencia en sincronización | Bajo | 30s es aceptable para catálogo (no es tiempo real) |
| Almacenamiento SQLite crece | Bajo | Limitar a tools + kanbans, no almacenar historial |

---

## 6. Dependencias

- VPS debe exponer `GET /api/tools` y `GET /api/kanbans` (ya existen)
- Gateway debe tener JWT token para autenticarse contra VPS (ya existe en `httpclient`)
- SQLite local ya tiene tabla `tools` (ver `localstore/store.go`)

---

## 7. RFC — Cambios Técnicos

### 7.1 Backend Gateway (Go)

#### Nuevos archivos
- `internal/localstore/kanban_store.go` — CRUD de kanbans en SQLite
- `internal/web/handlers_tools.go` — Handler para `/api/tools`
- `internal/web/handlers_kanbans.go` — Handler para `/api/kanbans`
- `internal/sync/tool_sync.go` — Sincronización de tools + kanbans desde VPS

#### Archivos modificados
- `internal/web/server.go` — Registrar nuevas rutas
- `internal/web/static/index.html` — Agregar pestañas y sección de herramientas
- `internal/web/static/style.css` — Estilos para cards de herramientas
- `internal/localstore/store.go` — Agregar `GetTools()` y `GetToolByID()`

#### Nuevas tablas SQLite
```sql
CREATE TABLE IF NOT EXISTS kanbans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id TEXT NOT NULL,
    code TEXT NOT NULL,
    zone TEXT NOT NULL,
    description TEXT,
    standard_pack INTEGER NOT NULL DEFAULT 1,
    max_production INTEGER NOT NULL DEFAULT 1,
    last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(company_id, code)
);
```

### 7.2 Frontend Gateway (HTML/HTMX)

#### Cambios en `index.html`
- Agregar barra de navegación por pestañas
- Sección "Herramientas" con grid de cards
- Auto-refresh cada 30 segundos
- Indicador de última sincronización

#### Estilos CSS
- Grid responsive para cards de herramientas
- Badges de estado (colores)
- Indicador de sincronización

---

## 8. Estimación

| Fase | Tiempo | Dependencias |
|------|--------|-------------|
| Backend: kanban_store + handlers | 2-3h | Ninguna |
| Backend: sync desde VPS | 2-3h | VPS endpoints |
| Frontend: pestañas + cards | 3-4h | Backend handlers |
| Testing | 1-2h | Todo lo anterior |
| **Total** | **8-12h** | |

---

## 9. Diagrama de Secuencia

```
Gateway (Raspberry Pi)          VPS (Cloud)
        │                           │
        │  GET /api/tools (JWT)     │
        │ ─────────────────────────>│
        │ <─────────────────────────│
        │  { tools: [...] }         │
        │                           │
        │  GET /api/kanbans (JWT)   │
        │ ─────────────────────────>│
        │ <─────────────────────────│
        │  { kanbans: [...] }       │
        │                           │
        │  Guardar en SQLite local  │
        │                           │
        │  Servir /api/tools        │
        │  (datos locales)          │
        │                           │
        │  Web UI muestra cards     │
```
