# PRD: Gateway UX Improvements

**Proyecto**: amg-rfid-gateway
**Fecha**: 2026-04-30
**Estado**: Borrador — Listo para revisión

---

## 1. Descripción General

### 1.1 Problema

La experiencia de uso del gateway en Raspberry Pi tiene 3 fricciones:

1. **Acceso complicado**: Para ejecutar el TUI hay que escribir `tui --config $(brew --prefix)/etc/amg-rfid-gateway/config.yaml`. El usuario tiene que saber dónde está el config y recordar el flag cada vez.

2. **Sin identidad visual**: El TUI arranca directamente al menú principal sin logo ni branding. No hay forma de saber qué versión se está ejecutando.

3. **Configuración por terminal**: Para cambiar cualquier setting hay que salir del TUI, abrir `nano`, editar el YAML manualmente, y reiniciar. Esto es propenso a errores de sintaxis y frustrante para usuarios no técnicos.

### 1.2 Solución

1. **Comando global `amg-rfid-gateway`**: El binario busca el config automáticamente en ubicaciones estándar (brew prefix, /etc, ./). Sin flags obligatorios.

2. **Splash screen con logo**: Al arrancar el TUI, mostrar un logo ASCII art de chip RFID con ondas + versión + estado de conexión.

3. **Pantalla de Settings en TUI**: Nueva sección "Settings" en el menú principal donde el usuario puede editar todos los campos del config sin salir del TUI.

### 1.3 Objetivos

| Objetivo | Métrica |
|----------|---------|
| Arrancar gateway sin flags | `amg-rfid-gateway` funciona sin `--config` |
| Logo visible al arrancar | Splash screen con logo + versión por 2 segundos |
| Editar config desde TUI | 100% de campos editables sin usar nano |
| Guardar config sin reiniciar | Changes aplicados en runtime sin restart |

---

## 2. Requisitos

### REQ-001: Auto-detección de config

El binario `gateway` y `tui` DEBEN buscar el config automáticamente en este orden:
1. `$GATEWAY_CONFIG` (env var)
2. `./config.yaml` (directorio actual)
3. `$(brew --prefix)/etc/amg-rfid-gateway/config.yaml` (Homebrew)
4. `/etc/amg-rfid-gateway/config.yaml` (sistema)

Si no encuentra ninguno, mostrar error con mensaje claro indicando dónde buscar.

### REQ-002: Comando global `amg-rfid-gateway`

Homebrew DEBE crear un symlink o alias `amg-rfid-gateway` que ejecute el TUI con el config auto-detectado.

El usuario escribe:
```bash
amg-rfid-gateway
```

No necesita flags ni paths.

### REQ-003: Splash screen con logo

Al arrancar el TUI, mostrar una splash screen con:
- Logo ASCII art (chip RFID con ondas)
- Nombre del gateway
- Versión actual
- Estado de conexión con el VPS
- Mostrar por ~2 segundos antes de ir al menú principal

Logo propuesto:
```
    ╭──────────╮
 ~~~│  ◉   ◉  │~~~
 ~~~│          │~~~
 ~~~│  (((A))) │~~~
 ~~~│          │~~~
    ╰──────────╯
   AMG RFID Gateway
      v0.1.0
```

### REQ-004: Pantalla Settings en TUI

Nueva opción "Settings" en el menú principal del TUI que muestre:
- Gateway ID (editable)
- Company ID (editable)
- Cloud URL (editable)
- Antenas (lista editable: IP, puerto, zona)
- Log Level (selector)
- Queue Cap (editable)

Cada campo editable con:
- Nombre del campo
- Valor actual
- Instrucciones de edición (Enter para editar, Esc para cancelar)

### REQ-005: Guardar config desde TUI

Al modificar un campo en Settings:
- Cambios se aplican en memoria inmediatamente
- Opción "Save" escribe el YAML al disco
- Confirmar antes de sobreescribir
- Mostrar mensaje de éxito/error

### REQ-006: Hot-reload de config

Al guardar config desde TUI:
- No reiniciar el gateway
- Recargar config en runtime
- Aplicar cambios a nuevas conexiones
- Mantener conexiones existentes hasta que se reconecten

---

## 3. Fuera de Alcance

- Configuración web (web UI) — futuro
- Configuración por mobile app — no aplica
- Multi-idioma — español solamente
- Import/export de config — futuro
- Validación avanzada de IPs (solo formato básico)

---

## 4. Riesgos

| Riesgo | Impacto | Mitigación |
|--------|---------|------------|
| YAML corrupto al guardar | Alto | Validar antes de escribir, backup automático |
| Config reload rompe conexiones | Medio | Solo aplicar a nuevas conexiones |
| Homebrew path no detectado | Bajo | Fallback a ./config.yaml |

---

## 5. Dependencias

- Ninguna dependencia externa
- Usa librerías ya incluidas (Bubbletea, Lipgloss, yaml.v3)
