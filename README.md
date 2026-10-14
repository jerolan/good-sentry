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
- Transacciones con promedio, p50/p95 y trace ID consultables por API.
- Logs con atributos tipados, filtros, deduplicación y correlación por trace ID consultables por API.
- Interfaz responsive de proyectos y errores, probada en Chromium a 320, 768 y 1440 píxeles.
- SDK oficial de Go `v0.50.0` contra el servidor nativo y contra la imagen de Compose, y Python `2.72.0` contra Compose. Se verificaron excepciones, transacciones y logs recuperando los datos guardados.

El compose actual desactiva uptime porque su implementación está pendiente. El proceso rechaza MCP, DuckDB, admin, OpenAPI, Valkey o uptime activados mientras no estén disponibles. Los tipos de envelope aún no soportados se conservan y se indican en `unsupportedItems`; eso no equivale a implementar su funcionalidad. Los logs desactivados se excluyen deliberadamente y producen un aviso de rate limit para sus categorías.

Node `11.7.0` ya pasa errores y logs. Su prueba de trazas detectó el nuevo transporte de spans v2 y está pendiente de la integración documentada en la especificación. La matriz del SDK oficial de navegador también sigue pendiente.

Siguen pendientes, entre otras funciones, alertas y scheduler, releases y cargas de source maps, simbolicación nativa, merge/unmerge, invitaciones, recuperación de contraseñas, integraciones y varias pantallas. El consumidor puro de source maps tiene pruebas de ubicación y contexto, pero aún requiere conectar su almacenamiento y API. No se ha medido el objetivo de memoria de 256-512 MB bajo carga.

## Configuración

`DATABASE_URL`, `SECRET_KEY` y `GOODSENTRY_DOMAIN` controlan almacenamiento, sesiones y DSN. También se aceptan `GLITCHTIP_DOMAIN` y los alias `GLITCHTIP_ENABLE_*`. Los nombres `GOODSENTRY_*` tienen prioridad. `PORT` vale 8000 en el proceso; el compose permite cambiar el puerto publicado mediante `PORT`. Si cambias ese puerto, ajusta también el dominio público.

`MAX_BODY_BYTES` limita el cuerpo comprimido y descomprimido, por defecto 32 MiB. `GOODSENTRY_ENABLE_LOGS=false` desactiva su ingesta. `VALKEY_URL` debe quedar vacío en este incremento. `SECRET_KEY` debe tener al menos 32 caracteres y no ser el valor de ejemplo. El compose usa credenciales PostgreSQL locales; `POSTGRES_PASSWORD` permite sustituirlas.

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

## Procedencia

El desarrollo usa documentación pública de formatos, APIs y funciones, además de tráfico de clientes oficiales ejecutados como cajas negras. No se ha consultado ni traducido implementación de los servidores Sentry, Relay o GlitchTip. Las fuentes y los límites de compatibilidad se registran en la especificación.
