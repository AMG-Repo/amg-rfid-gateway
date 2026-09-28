# Raspberry: prueba local controlada (plan ODD)

## Resultado buscado

Llegar a una prueba breve y reversible del gateway en la Raspberry, **sin lector ni VPS**, con observación y apagado comprobables. Este plan no declara listo el despliegue Homebrew, un piloto ni producción: `DEVICE-GATE` sigue **NO-GO** y `ApplyEligible=false`. La prueba en el dispositivo requiere decisiones y autorizaciones posteriores, separadas de los PRs.

## Alcance y límites

- Base de integración: `main` sincronizado en `f75bfa234bce6147508591e9934ed6cc1f7c736f` al redactar este plan. Antes de crear cada PR, confirmar de nuevo rama predeterminada, base y diff.
- El último tag local es `v0.6.5`; `main` está 28 commits por delante. La observación histórica de Raspberry corresponde a v0.6.5, no prueba lo que ejecutaría un build de `main`.
- Ruta objetivo: **demo temporal y aislada**, no transición del servicio Homebrew instalado. No usar `/opt` ni los remedios demo rechazados v1/v2/v3; no asumir que `homebrew-deploy plan` puede aplicar cambios (hoy devuelve `unsupported_action`). No reutilizar configuración o datos instalados sin una decisión explícita y evidencia de acceso/aislamiento.
- Ninguna tarea autoriza SSH, sudo, Brew, instalación, cambio de configuración, start/enable, ejecución del gateway, conexión al lector/VPS, push, PR ni merge. Cada operación remota requiere alcance y autorización propios.
- Sin épica. Cuando se vaya a publicar cada PR, vincularlo a **un issue aprobado propio**; no crear issues ni PRs por este plan. Cada PR lleva exactamente una etiqueta `type:*` y los checks requeridos por el repositorio.

## PRs propuestos, en orden

| PR / ODD | Entrega revisable y criterio de cierre | Dependencia y fuera de alcance |
| --- | --- | --- |
| **1. Preflight sin secretos** (`docs/raspberry-demo-preflight`) | Contrato/guía de inventario de solo lectura: identidad de build y proceso, config por categorías y permisos sin valores secretos, rutas/datos/socket, estado de unidad, listeners y red, antenas y límites de evidencia. Checklist de rechazo y registro de salida/errores sin afirmar que los datos históricos sean actuales. Verificación: revisión de seguridad del texto, `git diff --check` para tracked y chequeo directo de whitespace para archivos aún no rastreados. | Independiente. No se ejecuta SSH en este PR; no modifica dispositivo. |
| **2. Modo demo sin egreso** (`feat/raspberry-demo-offline`) | Modo explícito y fail-closed: no arrancar sync de gateway ni herramientas, no construir conexiones a VPS, no iniciar antenas/lector; configuración inválida rechaza arranque antes de efectos. Pruebas deterministas de los caminos negativos y arranque/parada con fakes que comprueben ausencia de clientes/diales. | Depende de PR 1 y de la decisión sobre el inventario actual; no es prueba de aislamiento del host. Si no cabe con pruebas/docs en ≤400 líneas cambiadas, hacer **una** partición honesta por comportamiento, no comprimir pruebas. |
| **3. Superficies locales privadas** (`feat/raspberry-demo-local-io`) | En modo demo, rechazar bindings no loopback; exigir rutas seleccionadas y privadas para datos/config/socket, verificar permisos y dueño efectivos y no recurrir al socket compartido `/tmp/amg-gateway.sock`. Pruebas de permisos, sustitución, fallos de startup y teardown sin tocar rutas Homebrew instaladas. | Depende de PR 2; no crea unidad systemd ni hace migración de ownership. Dividir listener y rutas en dos PRs solo si la partición conserva unidades completas y supera presupuesto. |
| **4. Procedimiento y evidencia de smoke test** (`docs/raspberry-demo-runbook`) | Guía de preflight → aprobación humana → arranque temporal aislado → health/TUI local según alcance → prueba de **ausencia** de lector/VPS/egreso → parada y verificación de limpieza. Lista condiciones de aborto, evidencia sensible a redactar, versión/binario exactos, resultado y rollback humano; puede incorporar tests herméticos del chequeo si hacen falta. | Depende de PRs 2–3. Publicar guía no autoriza ejecutarla, ni sustituye verificación fresca de red y dispositivo. |

Cada PR es un work unit con pruebas y documentación del comportamiento en la misma entrega, mensaje Conventional Commit y revisión independiente proporcionada al riesgo. Objetivo: ≤400 líneas añadidas + eliminadas y revisión ~≤60 minutos; si una unidad cohesiva supera 400 después de **una** partición razonable, consultar al mantenedor sobre `size:exception` en vez de recortar cobertura. Estrategia propuesta: PRs independientes a la rama predeterminada actualizada (`stacked-to-main`), integrados en orden; verificar base limpia antes de cada uno, nunca acumular diffs ajenos.

## Gates del dispositivo (no son PRs)

1. **Después de PR 1, antes de diseñar PR 2:** acordar alias/cuenta, ventana y **consulta exacta de solo lectura**; obtener autorización específica. No mostrar config ni secretos. Registrar exit status, límites y frescura: observaciones de septiembre no son un preflight. Si no se autoriza la consulta, PR 2 queda condicionado a hipótesis explícitas, no a estado supuesto del dispositivo.
2. **Antes de la prueba, refrescar evidencia:** seleccionar binario/release y entorno efímero sin reutilizar datos/credenciales del Homebrew existente; demostrar confinamiento de red **fuera de la aplicación**, permisos y paths efectivos. Si no hay prueba de ausencia de egreso/lector/VPS, mantener NO-GO.
3. Someter un procedimiento concreto de prueba y reversión a aprobación separada. Solo entonces ejecutar un smoke test mínimo, sin servicio persistente ni enable; verificar salida, listeners, trazas permitidas y parada. Un fallo implica detener y volver a NO-GO.

El trabajo de servicio systemd dedicado, recibo Homebrew, ownership de datos y recovery durable sigue en `odd/tasks/homebrew-deployment-hardening.md` y documentos asociados: es una vía distinta para despliegue persistente/piloto, no se declara resuelta por una demo temporal.

## Estado y siguiente decisión

- [x] Usuario confirmó avanzar por la ruta de demo temporal aislada y pidió issues individuales, sin épica. Con autorización exacta para GitHub `AMG-Repo/amg-rfid-gateway` y la credencial local del repositorio, se crearon y verificaron por readback [#89](https://github.com/AMG-Repo/amg-rfid-gateway/issues/89), [#90](https://github.com/AMG-Repo/amg-rfid-gateway/issues/90), [#91](https://github.com/AMG-Repo/amg-rfid-gateway/issues/91) y [#92](https://github.com/AMG-Repo/amg-rfid-gateway/issues/92). Están abiertos; el usuario autorizó expresamente aplicar `status:approved` solo a #89, y la lectura posterior confirmó exactamente esa etiqueta. #90–#92 siguen sin aprobación.
- [ ] **PR 1 / #89 — Preflight sin secretos.** Rama local `docs/raspberry-demo-preflight`; guía `docs/raspberry-demo-preflight.md` en preparación. Revisión independiente PASS para el borrador; se corrigieron sus dos caveats de enlace y chequeo de untracked. El usuario autorizó un commit local de estos dos archivos, sin push/PR/merge. Cierre de la tarea pendiente de verificación final y entrega posterior del PR.
- [ ] PRs siguientes #90 (sin egreso), #91 (IO local privado), #92 (runbook); cada uno se inicia después de su dependencia y decisión del operador.
- [ ] Acordar y autorizar individualmente preflight remoto, publicación de cada PR y eventual ejecución en la Raspberry.
