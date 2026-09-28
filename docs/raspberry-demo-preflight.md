# Preflight de Raspberry para una demo temporal

**Decisión inicial: NO-GO.** Esta guía define la evidencia mínima para *evaluar* una futura prueba local sin lector ni VPS. No es un comando para ejecutar, no diagnostica el estado actual de la Raspberry y no autoriza instalación, preparación, arranque ni tráfico de red. El [plan ODD](../odd/tasks/raspberry-controlled-demo.md) distingue esta demo del despliegue Homebrew persistente.

## Antes de consultar el equipo

1. Acordar con el operador destino exacto, cuenta, canal autenticado, ventana temporal, propósito y **texto exacto de cada consulta**. Obtener autorización específica antes de conectarse; no extenderla a consultas siguientes.
2. Revisar que las consultas no requieran `sudo`, Brew, ejecución de gateway/TUI, comandos que arranquen o habiliten unidades, ni lecturas que impriman configuración, entorno, credenciales o contenido de datos. Usar límites de tiempo y salida. Un comando llamado «de lectura» puede tener efectos colaterales; revisar cada herramienta y sus argumentos antes de aprobarla.
3. Definir cómo se obtendrán códigos de salida reales, identificador de arranque, hora de captura, alcance de servicios/procesos/puertos y redacción. Si una herramienta oculta el exit status o falla la captura, marcar **desconocido**, nunca inferir éxito de una línea `readback=complete`.

El historial de septiembre registró un binario v0.6.5 y un gateway detenido, pero no constituye preflight actual. `main` cambió después de ese tag. Ninguna coincidencia de bytes de release autentica por sí sola un recibo Homebrew instalado ni prueba que el binario elegido para una demo sea el que se ejecutará.

## Inventario mínimo, solo categorías seguras

| Evidencia requerida | Registrar sin divulgar secretos | Refusar o dejar como desconocido si... |
| --- | --- | --- |
| Destino y alcance | Identidad autenticada del host/cuenta, arquitectura, boot ID, instante y exit status por consulta; versión/binario candidato por identidad y digest comparado con artefacto fijado. | El host, el build, la procedencia o cualquier exit status no pueden comprobarse. No ejecutar el binario solo para obtener versión. |
| Configuración | Identidad y metadatos del archivo bajo lectura acotada sin seguir symlinks; **categorías**, no valores: permisos seguros/inseguros, cero antenas habilitadas, ruta de datos seleccionada/otra, socket privado explícito/default, bindings loopback/otros y destinos de sync presentes/ausentes. | El archivo cambia durante la lectura, es accesible a lectores no previstos, contiene valores ambiguos, falta la configuración demo seleccionada o aparece un default compartido. No imprimir YAML ni tokens. |
| Datos y runtime | Existencia/tipo, dueño y modo de directorios seleccionados; identidad de ancestros y nombre de socket; distinguir candidato libre de socket seguro y ciclo de vida probado. | Hay symlink, ruta inesperada, escritores no confiables, permiso ambiguo, datos instalados compartidos, socket ocupado o no se conoce quién crea/limpia runtime. |
| Proceso y manager | Identidad de procesos gateway/TUI, estado de **unidades exactas consultadas**, manager/boot, carga/actividad/enablement y presencia de overrides; solo metadatos redactados. | Algún proceso o unidad objetivo ya está activo, cambia la identidad o no se conoce el alcance. Unas pocas unidades ausentes no prueban ausencia de otros servicios. |
| Listeners y conectividad | Puertos/transportes consultados, direcciones de escucha por categoría (loopback/wildcard/otra), estado de aislamiento externo revisado por separado. | Aparece listener no aprobado o no se puede verificar confinamiento. Listeners ausentes no prueban ausencia de egreso hacia VPS/lector. |

No recopilar valores de `config.yaml`, variables de entorno, argumentos completos, credenciales, tokens, IP privadas ni contenido de bases de datos. Cualquier salida no redactada se trata como incidente de evidencia: detener consulta y no publicarla en issue/PR/log. Un error de permisos no justifica elevar privilegios automáticamente.

## Resultado y decisión

Para cada observación anotar: `alcance`, `fuente`, `timestamp`, `boot_id`, `exit_status`, `identidad_estable`, `categoría`, `límite` y `resultado` (`observado`, `rechazado`, `desconocido`). Adjuntar solo resumen saneado y la lista de superficies **que sí** se consultaron. No transformar «no encontrado entre cuatro unidades / dos puertos» en «no existe ningún servicio / conexión».

- **NO-GO** ante estado activo inesperado, config insegura o incongruente, path/socket no privado, build distinto, cuenta sin acceso probado, aislamiento de red no demostrado, resultado ambiguo, exit status desconocido o evidencia caducada.
- Un preflight favorable es **necesario pero no suficiente**: antes de cualquier arranque hay que revisar un plan efímero con binario/config/datos aislados, confinamiento de red independiente, condiciones de aborto y parada/readback; solicitar otra autorización explícita. Cero antenas habilitadas no impide que el proceso inicie componentes de sincronización.
- No usar este preflight como `homebrew-deploy apply`: el CLI actual no tiene apply/rollback ni autoridad de activación. Para servicio persistente, receipt, identidad no-root, migración de ownership y recuperación sigue rigiendo el [contrato Homebrew](homebrew-deployment-contract.md); el [tracker de despliegue](../odd/tasks/homebrew-deployment-hardening.md) mantiene DEVICE-GATE NO-GO.

## Siguiente aprobación

Preparar fuera de este documento una consulta concreta y limitada que implemente cada categoría sin imprimir secretos. Presentar **texto exacto**, destinatario, cuenta, límite y comportamiento de fallo al operador; no conectarse hasta recibir autorización. Si la evidencia recibida es parcial, cerrar la consulta sin seguirla automáticamente y reportar NO-GO con sus límites.
