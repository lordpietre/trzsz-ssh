# Plan: Integración FRP Proxy en tssh

## Resumen

Añadir opción "FRP Proxy" en menú contextual del host (debajo de "Tunnels") para:
1. Compilar frps/frpc desde el fork local (`frp/`)
2. Instalar/configurar FRP local y remoto vía SSH
3. Escanear puertos y seleccionar servicio a exponer
4. Iniciar proxy FRP en ambas direcciones

## Archivos

| Archivo | Tipo | Propósito |
|---|---|---|
| `tssh/frp_proxy.go` | **NUEVO** | Toda la lógica FRP (estados, handlers, renders, procesos, instalación) |
| `tssh/host_model.go` | MODIFICAR | +1 campo struct, +1 item menú, +1 dispatch, +1 render |

## Estados del TUI

```
menu → direction → setup → scan_results → confirm → list
```

- `menu` — lista proxies guardados + opción "New"
- `direction` — elegir Remote→Local (R2L) o Local→Remote (L2R)
- `setup` — progreso: compilar→configurar→iniciar→detectar IP→instalar remoto
- `scan_results` — puertos escaneados, seleccionar servicio
- `confirm` — nombre del proxy, puerto expuesto
- `list` — proxies activos, opciones delete/new

## Flujo R2L (Remote → Local)

1. Compilar `frps` + `frpc` estáticos desde `frp/cmd/` (si no existen)
2. Generar token aleatorio (`crypto/rand`)
3. Escribir `~/.tssh/frps_<alias>.toml`
4. Iniciar `frps` local en puerto 7000
5. Detectar IP pública vía ifconfig.me/api.ipify.org
6. Subir binario `frpc` al remoto vía SSH (pipe `cat >`)
7. Escribir `~/.tssh/frpc_<alias>.toml` en remoto
8. Iniciar `frpc` en remoto (nohup)
9. Escanear puertos del remoto → seleccionar servicio
10. Añadir `[[proxies]]` a config remota y recargar

## Flujo L2R (Local → Remote)

1. Compilar `frps` + `frpc` estáticos (si no existen)
2. Generar token aleatorio
3. Subir `frps` al remoto, iniciarlo
4. Detectar IP del remoto
5. Escribir `~/.tssh/frpc_<alias>.toml` local
6. Iniciar `frpc` local
7. Escanear puertos locales → seleccionar servicio
8. Añadir `[[proxies]]` a config local y recargar

## Estructuras de datos

```go
type frpProxyEntry struct {
    Alias       string `json:"alias"`
    Name        string `json:"name"`
    Direction   string `json:"direction"`    // "r2l" | "l2r"
    FrpsAddr    string `json:"frps_addr"`
    FrpsPort    int    `json:"frps_port"`
    Token       string `json:"token"`
    ServicePort int    `json:"service_port"`
    ExposedPort int    `json:"exposed_port"`
    Active      bool   `json:"-"`
    LocalCmd    *exec.Cmd `json:"-"`
}
```

Persistencia en `~/.tssh_frp` (mismo patrón que tunnels).

## Mensajes (tea.Msg)

- `frpSetupProgressMsg{step, err}` — progreso de instalación/configuración
- `frpScanResultMsg{ports, err}` — resultado de escaneo local
- `frpStartResultMsg{entry, err}` — proxy iniciado
- `frpStopResultMsg{entry, err}` — proxy detenido

## Compilación de binarios FRP

```go
func frpCompileIfNeeded() error {
    srcDir := filepath.Join(exeDir, "frp")  // o cwd/frp
    go build -o ~/.tssh/bin/frps ./frp/cmd/frps
    go build -o ~/.tssh/bin/frpc ./frp/cmd/frpc
}
```

Binarios guardados en `~/.tssh/bin/`. Se busca en: PATH → junto al binario → `~/.tssh/bin/`.

## Modificaciones a host_model.go

| Línea | Cambio |
|---|---|
| ~167 | Añadir `frp frpProxyState` al struct `hostModel` |
| ~312 | Añadir item "FRP Proxy" en `getContextItems()` |
| ~574 | Añadir `m.frp.active` en `handleKey()` |
| ~531 | Añadir casos para mensajes FRP en `Update()` |
| ~1271 | Añadir `else if m.frp.active` en `View()` |
| ~1692 | Añadir `renderFrpView()` en dispatch |

## Dependencias externas

- Go toolchain (para compilar FRP)
- ss o netstat (para escaneo local de puertos)
- Las mismas que tssh para SSH (golang.org/x/crypto)
