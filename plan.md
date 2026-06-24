# Plan: Migración completa a ncurses + TSSH 2.0

Basado en el documento de Arquitectura Técnica TSSH 2.0 y los issues
actuales (panic en renderActions, navegación de acciones, new host fuera del TUI).

---

## Fase 0 — Correcciones inmediatas ✓

| # | Tarea | Archivo | Estado |
|---|-------|---------|--------|
| 0.1 | Panic `strings: negative Repeat count` en `renderActions` | `host_model.go` | ✓ FIXED |
| 0.2 | Navegación de acciones con ← → + Enter | `host_model.go` | ✓ FIXED |
| 0.3 | Ayuda (`?`) implementada (showHelp) | `host_model.go` | ✓ FIXED |
| 0.4 | ← → en listModel y menuModel | `tools.go`, `console.go` | ✓ FIXED |

---

## Fase 1 — Estabilizar TUI ncurses (fondo blanco siempre visible)

### 1.1 Fondo blanco consistente en todas las vistas
- **Qué**: Asegurar que `bgStyle` (fondo blanco `#15`) se aplique en TODA la pantalla,
  sin huecos ni líneas sin fondo.
- **Cómo**: En `View()`, cada línea debe renderizarse con `bgLine()` o `bgStyle`.
  Revisar `renderDetails`, `renderActions`, `renderHelp`.
- **Archivos**: `host_model.go`, `console.go`, `attach.go`, `tools.go`

### 1.2 Ancho de terminal correcto con códigos ANSI
- **Qué**: Toda medición de ancho visible debe usar `ansi.StringWidth()` o
  `lipgloss.Width()` que ignoran códigos ANSI.
- **Cómo**: Buscar todos los `runewidth.StringWidth()` y reemplazar donde
  la entrada pueda contener ANSI. Ya corregido en `renderActions`.
- **Archivos**: `host_model.go`, `console.go`, `attach.go`

### 1.3 Padding seguro (nunca negativo)
- **Qué**: Toda llamada a `strings.Repeat(" ", N)` debe tener `max(N, 0)`.
- **Cómo**: Crear helper `repeatSpace(n int)` que devuelva vacío si n < 0.
- **Archivos**: `host_model.go`, `console.go`, `attach.go`

### 1.4 Scroll suave en lista de hosts
- **Qué**: Al llegar al final de la lista, el cursor no debe saltar.
- **Cómo**: Verificar `scrollOffset` y `availableHeight` en `View()`.
- **Archivos**: `host_model.go`

---

## Fase 2 — New host dentro del TUI ncurses

### 2.1 Formulario de nuevo host como modelo Bubble Tea
- **Qué**: Reemplazar las llamadas a `promptTextInput`, `promptPassword`,
  `promptList` (que lanzan programas Bubble Tea separados) por un modelo
  interno que renderice el formulario dentro del mismo TUI ncurses.
- **Cómo**: Crear `newHostFormModel` en `host_model.go` (o nuevo archivo
  `host_new.go`) con campos editables inline.
  - Campo `ConfigPath` (texto)
  - Campo `HostAlias` (texto)
  - Campo `HostName` (texto)
  - Campo `HostPort` (numérico, default 22)
  - Campo `UserName` (texto)
  - Campo `Password` (contraseña, oculta)
  - Botón `[Save]` y `[Cancel]`
- **Navegación**: Tab/Shift+Tab entre campos, ↑↓ en listas, Enter para aceptar.

### 2.2 Integración con el flujo actual
- **Qué**: En lugar de `m.done = true; m.result.newHost = true` y reiniciar
  el TUI, el `newHostFormModel` se ejecuta como un sub-modelo dentro del
  mismo `tea.Program`.
- **Cómo**: Usar `tea.Sequence` o cambio de modelo en `Update()`.
  Al guardar, vuelve a la lista de hosts con el nuevo host visible.
- **Archivos**: `host_model.go`, `tools_new_host.go`

### 2.3 Validación inline
- **Qué**: Mostrar errores de validación (host duplicado, puerto inválido,
  lookup DNS fallido) como texto rojo debajo del campo, no como popup.
- **Cómo**: Usar `err` field en el modelo, renderizado condicional.
- **Archivos**: `host_new.go` (nuevo)

### 2.4 Health-check visual
- **Qué**: Al introducir HostName, hacer `net.DialTimeout` no-bloqueante
  y mostrar icono verde/rojo.
- **Cómo**: `tea.Tick` con timeout, actualizar estado.
- **Archivos**: `host_new.go`

---

## Fase 3 — Tema ncurses completo y consistente

### 3.1 Paleta ncurses clásica
```
Fondo:      Blanco (ANSI 15, #FFFFFF)
Texto:      Negro (ANSI 0, #000000)
Cursor:     Azul oscuro (ANSI 4, #0000AA)
Selección:  Verde (ANSI 2, #00AA00)
Ayuda:      Gris (ANSI 8, #555555)
Título:     Blanco sobre azul
Bordes:     Azul oscuro (ANSI 4)
```
- **Archivos**: `host_model.go` (`initStyles()`), `console.go`, `attach.go`, `tools.go`

### 3.2 Cursor ncurses sólido (`▐█`)
- **Qué**: Ya implementado en `renderHost`. Verificar que funcione con el
  tema seleccionado.
- **Archivos**: `host_model.go`

### 3.3 Bordes y separadores ncurses
- **Qué**: Usar caracteres de dibujo de caja (`─│┌┐├┤└┘`) de forma consistente.
  Ya implementado en View().
- **Archivos**: `host_model.go`, `console.go`, `attach.go`

### 3.4 Temas legacy (tiny/simple/table)
- **Qué**: Los temas en `theme.go` quedaron como dead code tras migrar a
  Bubble Tea. Decidir si eliminarlos o adaptarlos como variantes del tema
  ncurses.
- **Propuesta**: Eliminar `theme.go` y mover la lógica de `promptThemeLayout`
  a un selector de tema ncurses (clásico, compacto, tabla).
- **Archivos**: `theme.go`, `config.go`

### 3.5 Consola de escape (`~`) con tema ncurses
- **Qué**: La consola `menuModel` en `console.go` tiene su propio estilo
  (fondo morado oscuro). Migrar a fondo blanco ncurses.
- **Archivos**: `console.go`

### 3.6 Attach TUI con tema ncurses
- **Qué**: El `attachModel` en `attach.go` tiene su propio estilo
  (fondo negro, bordes morados). Migrar a fondo blanco ncurses.
- **Archivos**: `attach.go`

### 3.7 ListModel y otros utilities con tema ncurses
- **Qué**: `listModel`, `textInputModel`, `passwordModel` en `tools.go`
  usan colores ANSI básicos. Migrar a la paleta ncurses.
- **Archivos**: `tools.go`

---

## Fase 4 — Sesión Manager + Drivers modulares

### 4.1 Session Manager (núcleo)
- **Qué**: Nuevo componente central que orquesta todas las operaciones.
  Reemplaza la lógica dispersa actual.
- **Cómo**: 
  ```go
  type SessionManager struct {
      sessions  map[string]*Session
      drivers   map[string]Driver
      tunnels   *TunnelManager
      discovery *DiscoveryManager
      config    *ConfigManager
  }
  ```
- **Archivos**: `internal/session/` (nuevo paquete)

### 4.2 Driver interface
- **Qué**: Interfaz común para todos los protocolos.
  ```go
  type Driver interface {
      Connect(params ConnParams) (Session, error)
      Disconnect(id string) error
      Shell(id string) (io.ReadWriter, error)
      Exec(id string, cmd string) ([]byte, error)
      Tunnel(params TunnelParams) error
  }
  ```
- **Archivos**: `internal/driver/` (nuevo)

### 4.3 SSH Driver refactorizado
- **Qué**: Extraer la lógica SSH actual en un `sshDriver` que implemente
  `Driver`. Separar autenticación, sesión, shell, túneles.
- **Cómo**: Mover `sshLogin`, `keepAlive`, `sshConnection` a un driver.
- **Archivos**: `internal/driver/ssh/` (nuevo)

### 4.4 Serial Driver (base)
- **Qué**: Soporte para `/dev/ttyUSB*`, `/dev/ttyS*` con config de
  baudrate, data bits, stop bits, paridad.
- **Cómo**: Usar `go.bug.st/serial` (o similar).
- **Archivos**: `internal/driver/serial/` (nuevo)

### 4.5 TCP Raw Driver (base)
- **Qué**: Conexiones TCP simples sin SSH (para depuración, APIs, PLCs).
- **Cómo**: `net.Dial` directo.
- **Archivos**: `internal/driver/tcp/` (nuevo)

---

## Fase 5 — Discovery, Túneles y Servicios

### 5.1 Discovery Manager
- **Qué**: Ejecutar `ss -tulpen` (o `netstat -tulpen`) vía SSH para
  detectar servicios remotos. Convertir cada servicio en objeto interno.
- **Cómo**: 
  ```go
  type Service struct {
      Name     string
      Port     uint16
      Protocol string
      Process  string
      Address  string
      Status   string
      Public   bool   // 0.0.0.0 vs 127.0.0.1
  }
  ```
- **Archivos**: `internal/discovery/` (nuevo)

### 5.2 Tunnel Manager
- **Qué**: Crear, detener, reiniciar, monitorizar túneles SSH (-L, -R, -D).
  Publicar servicios privados automáticamente.
- **Cómo**: Usar `ssh.Listen` y `ssh.Dial` directamente.
- **Archivos**: `internal/tunnel/` (nuevo)

### 5.3 Port Manager
- **Qué**: Asignar puertos locales automáticamente (evitar conflictos).
  Mantener tabla de asignaciones.
- **Archivos**: `internal/port/` (nuevo)

### 5.4 Service Manager
- **Qué**: Inventario persistente de servicios detectados por servidor.
  Evitar escaneos repetidos.
- **Archivos**: `internal/service/` (nuevo)

### 5.5 Connection Manager
- **Qué**: Estado de todas las conexiones activas (SSH, Serial, TCP,
  túneles). Tiempo activo, reconexiones, errores.
- **Archivos**: `internal/connection/` (nuevo)

### 5.6 Config Manager
- **Qué**: Persistencia en YAML de servidores, usuarios, perfiles,
  túneles, preferencias.
- **Archivos**: `internal/config/` (nuevo)

### 5.7 Log Manager
- **Qué**: Registro centralizado de eventos (conexiones, errores,
  túneles, discovery).
- **Archivos**: `internal/log/` (nuevo)

---

## Fase 6 — Interfaz ncurses completa (TUI 2.0)

### 6.1 Organización por recursos
```
┌──────────────────────────────────────┐
│  tssh — Session Manager              │  ← título
├──────────────────────────────────────┤
│  [Hosts] [Services] [Tunnels] [Logs] │  ← pestañas (← →)
├──────────────────────────────────────┤
│                                      │
│  ▐█ web-prod-1          [production] │  ← contenido dinámico
│    web-prod-2            [production]│
│    db-prod               [database]  │
│                                      │
├──────────────────────────────────────┤
│  ▲▼ Navigate  ←→ Tabs  Enter Open   │  ← barra de estado
└──────────────────────────────────────┘
```

### 6.2 Panel de Servicios
- **Qué**: Al seleccionar un host y entrar a "Services", mostrar lista de
  servicios detectados con iconos y colores.
- **Acciones contextuales**: Abrir, Crear túnel, Info, Favorito.

### 6.3 Panel de Túneles
- **Qué**: Lista de túneles activos con estado, puerto local → remoto.
- **Acciones**: Detener, Reiniciar, Eliminar.

### 6.4 Panel de Logs
- **Qué**: Logs en tiempo real de conexiones, túneles, errores.

### 6.5 Barra de pestañas navegable
- **Qué**: ← → para cambiar entre paneles (Hosts, Services, Tunnels, Logs).
- **Cómo**: `tabCursor` similar a `actionCursor`.

---

## Resumen de archivos

| Fase | Archivos nuevos | Archivos modificados |
|------|----------------|---------------------|
| 0 | — | `host_model.go`, `tools.go`, `console.go` |
| 1 | — | `host_model.go`, `console.go`, `attach.go`, `tools.go` |
| 2 | `host_new.go` | `host_model.go`, `tools_new_host.go` |
| 3 | — | `host_model.go`, `console.go`, `attach.go`, `tools.go`, `theme.go` |
| 4 | `internal/session/`, `internal/driver/` | `tssh/login.go`, `tssh/main.go` |
| 5 | `internal/discovery/`, `internal/tunnel/`, `internal/port/`, `internal/service/`, `internal/config/`, `internal/log/` | — |
| 6 | — | `host_model.go`, `tssh/main.go` |

---

## Prioridades inmediatas (ahora)

1. ✅ Fase 0 — Panics y navegación de acciones
2. ⬜ Fase 1 — Fondo blanco consistente, padding seguro
3. ⬜ Fase 2 — New host dentro del TUI (formulario inline)
4. ⬜ Fase 3 — Tema ncurses completo
5. ⬜ Fase 4+ — Session Manager y drivers (arquitectura TSSH 2.0)
