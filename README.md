# GoodSentry

Servidor de errores y observabilidad escrito en Go, desarrollado de forma independiente a partir de contratos públicos. El objetivo es reemplazar el destino de los SDK de Sentry cambiando el DSN, con una imagen de aplicación y PostgreSQL.

**En desarrollo. Todavía no alcanza la paridad funcional mínima solicitada con GlitchTip.** El alcance completo, las decisiones y las pruebas pendientes están en [SPEC.md](SPEC.md), [el plan](tasks/plan.md) y [las tareas](tasks/todo.md).

## Arranque local

Requiere Docker con Compose. En Windows, habilita la integración de Docker Desktop con tu distribución WSL2.

```sh
test -f .env || printf 'SECRET_KEY=%s\n' "$(openssl rand -hex 32)" > .env
docker compose up --build -d
```

Abre http://localhost:8000, crea la primera cuenta y organización, crea un proyecto y copia su DSN. No hay contraseña inicial compartida. Conserva `.env` entre reinicios. Para un dominio propio, configura `GOODSENTRY_DOMAIN` con el esquema, host y prefijo de ruta públicos. Ajusta las credenciales PostgreSQL antes de usar la instalación fuera del entorno local.

La imagen contiene el binario Go y la interfaz. Se ejecuta como usuario `65532:65532`, sin runtime de Node ni servicios web adicionales. PostgreSQL persiste en `pg-data`. La carpeta `/data` queda disponible para artefactos. Las migraciones se aplican al arrancar bajo un bloqueo y se comprueban sus checksums.

```sh
docker compose logs goodsentry
docker compose restart goodsentry
docker compose down
```

`down` conserva los volúmenes. No uses `down -v` si quieres conservar los datos.

## Funciones verificadas hasta ahora

- Primera cuenta, login/logout, sesiones, organización y proyectos con DSN.
- Ingesta `store` y `envelope`, autenticación SDK y cuerpos gzip, zlib, Brotli y Zstandard.
- Errores agrupados con contexto original, búsqueda, filtros, resolución, reapertura y comentarios.
- Transacciones con promedio, p50/p95 y trace ID en API y UI; la ventana actual es de 24 horas.
- Logs con atributos tipados, filtros, deduplicación y correlación por trace ID en API y UI. Los enteros de 64 bits se muestran sin redondear.
- Interfaz Tailwind de proyectos, errores, transacciones, logs y uptime, probada en Chromium a 320, 768 y 1440 píxeles.
- SDK oficiales Go `v0.50.0`, Python `2.72.0`, Node `11.7.0` y navegador `11.7.0`: excepciones, transacciones y logs verificados recuperando los datos guardados. El SDK de navegador se ejecutó en Chromium desde otro origen para comprobar CORS.
- Spans v2 de JavaScript con relaciones padre/hijo y deduplicación; solo los segmentos cuentan como transacciones en los agregados.
- Source maps de JavaScript: carga autenticada desde Projects, selección por release/distribution o Debug ID, archivo/línea/columna/contexto originales y conservación del stack minificado. Los datos y mapas persisten en PostgreSQL.
- Monitores HTTP HEAD/GET/POST, TCP y heartbeats con scheduler dentro del proceso Go, umbral de fallos, historial, pausa y recuperación. Dos réplicas coordinan las comprobaciones mediante PostgreSQL; sesiones e historial sobreviven reinicios.

El proceso rechaza MCP, DuckDB, admin, OpenAPI o Valkey activados mientras no estén disponibles. Los tipos de envelope aún no soportados se conservan y se indican en `unsupportedItems`; eso no equivale a implementar su funcionalidad. Los logs desactivados se excluyen deliberadamente y producen un aviso de rate limit para sus categorías.

Esta matriz cubre errores, transacciones y logs de esas versiones concretas. Adjuntos, otros formatos y versiones requieren sus propias verificaciones.

Siguen pendientes, entre otras funciones, alertas y sus entregas, monitores de certificados SSL, ciclo de releases y cargas mediante sentry-cli, OTLP nativo, simbolicación nativa, merge/unmerge, invitaciones, recuperación de contraseñas, integraciones y varias pantallas. El decoder de bundles del CLI está probado, pero sus endpoints de carga todavía no están conectados. No se ha medido el objetivo de memoria de 256-512 MB bajo carga. La imagen actual se ha verificado en Linux amd64.

Para un source map, abre su proyecto en Projects y despliega Source maps. Indica el nombre o URL del artefacto, selecciona el archivo y proporciona el release usado por el SDK o su Debug ID. La distribución debe coincidir con la del evento. Owners y admins pueden cargar mapas; los miembros pueden consultar sus metadatos. Un reintento idéntico no duplica el archivo; contenido distinto para la misma identidad devuelve conflicto. La carga nativa está limitada por `MAX_BODY_BYTES`; cada conjunto seleccionado admite hasta 100 mapas y un millón de segmentos. Los mapas indexados admiten hasta 32 niveles. El servidor utiliza solo fuentes incluidas en los mapas subidos.

## Configuración

`DATABASE_URL`, `SECRET_KEY` y `GOODSENTRY_DOMAIN` controlan almacenamiento, sesiones y DSN. También se aceptan `GLITCHTIP_DOMAIN` y los alias `GLITCHTIP_ENABLE_*`. Los nombres `GOODSENTRY_*` tienen prioridad. `PORT` vale 8000 en el proceso; el compose permite cambiar el puerto publicado mediante `PORT`. Si cambias ese puerto, ajusta también el dominio público.

`MAX_BODY_BYTES` limita el cuerpo comprimido y descomprimido, por defecto 32 MiB. `GOODSENTRY_ENABLE_LOGS=false` desactiva su ingesta. `VALKEY_URL` debe quedar vacío en este incremento. `SECRET_KEY` debe tener al menos 32 caracteres y no ser el valor de ejemplo. El compose usa credenciales PostgreSQL locales; `POSTGRES_PASSWORD` permite sustituirlas.

Uptime está habilitado por defecto. `GOODSENTRY_ENABLE_UPTIME=false` elimina sus rutas, navegación y workers. `MONITOR_WORKERS` permite entre 1 y 16 comprobaciones simultáneas por proceso, con 2 por defecto. Varias réplicas coordinan su trabajo mediante leases en PostgreSQL. Una comprobación cancelada por apagado se recupera al vencer su lease y no registra una caída falsa.

En Uptime, crea un monitor HTTP HEAD/GET/POST, TCP o un heartbeat. Los intervalos van de 1 a 86400 segundos y los timeouts de 1 a 60. El umbral exige fallos consecutivos antes de confirmar una caída; un resultado correcto recupera inmediatamente. POST envía un cuerpo vacío. En GET/POST, el contenido esperado es una subcadena literal dentro del primer MiB. Para TCP, usa `host:puerto` o `[IPv6]:puerto`. Los heartbeats reciben POST en su URL privada antes de cada intervalo, sin tiempo de gracia adicional. Pausar conserva el historial y rechaza heartbeats hasta reanudar. Owners y admins administran; los miembros consultan estado e historial.

Las comprobaciones bloquean destinos privados, loopback, link-local y multicast por defecto, también tras redirecciones. Para servicios de tu red interna configura `GOODSENTRY_UPTIME_ALLOW_PRIVATE_IPS=true`. Se acepta el alias `GLITCHTIP_UPTIME_ALLOW_PRIVATE_IPS`; el nombre de GoodSentry tiene prioridad. HTTPS verifica certificados. El historial distingue cada resultado del estado confirmado y muestra transiciones o todas las comprobaciones en páginas de 25.

## Desarrollo y verificación

Requiere Go 1.27. La interfaz se incorpora con `go:embed`. Tailwind CSS 4.3.3 compila `internal/server/web/styles.css` a `app.css`. El Dockerfile descarga el CLI oficial con checksum fijado, compila los estilos y excluye ese compilador de la imagen final. El CSS generado se guarda para permitir compilación local de Go sin herramientas frontend. Los SDK de prueba son clientes externos, no dependencias del servidor.

```sh
go test ./...
go vet ./...
go build -trimpath -o bin/goodsentry ./cmd/goodsentry
DATABASE_URL=postgres://goodsentry:goodsentry@localhost:5432/goodsentry go test -tags=integration ./...
CGO_ENABLED=1 go test -race ./...
```

Las pruebas de integración exigen PostgreSQL y crean esquemas aislados. El detector de carreras requiere compilador C. El cliente externo Go vive en un módulo separado:

```sh
cd tests/compat
GOODSENTRY_URL=http://localhost:8000 COMPAT_EMAIL=tu-correo COMPAT_PASSWORD=tu-password go run .
```

Usa una instancia desechable para las pruebas de onboarding y compatibilidad. El cliente crea proyectos y eventos. El harness `tests/compat/browser.mjs` usa Playwright externo y guarda su evidencia en `test-results/`, excluido de Git.

Los clientes Python y Node tienen dependencias fijadas en `python-requirements.txt` y `package-lock.json`, dentro de `tests/compat`. Ejecútalos con las mismas variables `GOODSENTRY_URL`, `COMPAT_EMAIL` y `COMPAT_PASSWORD`. `browser_sdk.mjs` requiere Playwright y Chromium externos; carga el bundle oficial de navegador 11.7.0 con SRI fijado, desde un origen de prueba separado, y permite seleccionar Playwright con `COMPAT_PLAYWRIGHT`. El harness de la UI usa `GOODSENTRY_TEST_URL`, `GOODSENTRY_TEST_EMAIL`, `GOODSENTRY_TEST_PASSWORD` y `PLAYWRIGHT_MODULE`. Estas dependencias no forman parte del servidor ni de su imagen.

`tests/compat/browser-uptime.mjs` añade aceptación de monitores. Necesita `GOODSENTRY_MONITOR_TARGET` con un servidor HTTP real y `GOODSENTRY_MONITOR_CONTROL_URL` que reciba POST JSON `{ "status": 200, "body": "uptime browser proof" }` para cambiar ese destino entre 200 y 503. Para cubrir TCP, miembros y funciones desactivadas, añade `GOODSENTRY_MONITOR_TCP_TARGET`, `COMPAT_MEMBER_EMAIL`, `COMPAT_MEMBER_PASSWORD` y `GOODSENTRY_DISABLED_URL`. La instancia de aceptación permite destinos privados explícitamente; el harness elimina solo los monitores que crea.

## Procedencia

El desarrollo usa documentación pública de formatos, APIs y funciones, además de tráfico de clientes oficiales ejecutados como cajas negras. No se ha consultado ni traducido implementación de los servidores Sentry, Relay o GlitchTip. Las fuentes y los límites de compatibilidad se registran en la especificación.
