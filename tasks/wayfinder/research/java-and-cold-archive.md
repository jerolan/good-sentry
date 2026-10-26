# Contexto Java y archivo frío en la referencia de GlitchTip

Investigación factual para Acordar los flujos que prueban equivalencia con
GlitchTip. No decide alcance, sacrificios ni aceptación del producto.
Consulta: 2026-10-25, America/Mexico_City. Se reutilizaron glitchtip-baseline.md y
goodsentry-evidence.md; no se repitió la auditoría de GoodSentry.

## Resultado

El contexto Java está anunciado como capacidad de GlitchTip 6.1, separado de
la captura de minidumps. Las páginas consultadas no fijan un flujo Java de
subida de fuentes ni versiones reproducibles que permitan declarar su
aceptación. Una prueba nativa Linux/amd64 no demuestra esta capacidad.

El archivo frío de logs sí está documentado expresamente. También hay
fuentes fechadas, anteriores a 6.2.6, que describen archivo de errores y
almacenamiento de spans en Parquet. El alcance no se reduce a transacciones
y spans. Que GoodSentry tenga ese último archivo no acredita logs o errores.

## Contexto Java: hechos públicos y límites

El [anuncio de 6.1, del 23 de marzo de 2026](https://glitchtip.com/blog/2026-03-23-glitchtip-6-1-released/),
sección de minidump y contexto fuente, dice: "6.1 now supports minidump and
source contexts for Java." No especifica SDK, JDK, herramienta de subida,
formato, asociación entre fuente y evento ni criterios visibles detallados.
El encabezado compartido no demuestra que contexto Java use minidumps.

La [guía Java](https://glitchtip.com/sdkdocs/java/) enseña a instalar
io.sentry:sentry con Gradle 8.+ o Maven LATEST, inicializar DSN y muestreo,
capturar una excepción y ejecutar flush antes de salir. No fija una versión
exacta ni documenta carga de fuentes o recuperación de contexto. Sus
selectores móviles no sirven como pin reproducible de cierre.

La [guía CLI](https://glitchtip.com/documentation/cli/) documenta source maps
JavaScript y carga de símbolos dSYM, PDB y ELF mediante debug-files upload.
No documenta en esa página un comando Java de source bundle. No inferir
que estos ejemplos basten para contexto Java. No se leyó código del CLI.

La [guía general de SDKs](https://glitchtip.com/sdkdocs/all-sdks/) remite a
fuentes y símbolos y describe captura por DSN, pero no completa el contrato
Java. La [guía de errores](https://glitchtip.com/documentation/error-tracking/)
explica source maps para JavaScript, que es otra capacidad.

Inferencia delimitada: para aceptar equivalencia de contexto Java haría
falta acordar una excepción Java de muestra, sus fuentes correspondientes
y qué contexto debe mostrarse en el frame. Es una propuesta de prueba,
no una regla detallada demostrada de GlitchTip. Sigue faltando un flujo
público suficientemente preciso, o una observación black-box de la versión
congelada, para determinar cliente, artefacto, asociación y resultado exacto.
La ausencia de esos detalles en estas páginas no prueba ausencia de soporte.

## Archivo frío: separar los tipos de datos

| Tipo | Hecho público | Fuente y límite |
|---|---|---|
| Logs | Tras el umbral caliente pasan a Parquet en S3 o filesystem local y siguen consultables desde UI y API. Las consultas combinan datos calientes y fríos. | [Guía de logs, Retention and Storage y Cold Storage](https://glitchtip.com/documentation/logs/). Es rolling; no demuestra ejecución de v6.2.6. |
| Errores | La configuración distingue plazo caliente y retención total. El anuncio 6.1 ofrece retener eventos antiguos; el artículo de abril describe copiar particiones y poder consultar un evento antiguo. | [Anuncio 6.1](https://glitchtip.com/blog/2026-03-23-glitchtip-6-1-released/), [artículo del 20 de abril](https://glitchtip.com/blog/2026-04-20-duckdb-parquet-archives/) y [configuración](https://glitchtip.com/documentation/install/). No establece que toda operación de triage tenga idéntica semántica sobre datos fríos. |
| Spans | El anuncio 6.1 exige DuckDB para spans y ofrece consultas mediante API/MCP. El artículo de abril describe spans almacenados solamente en Parquet. | [Anuncio 6.1](https://glitchtip.com/blog/2026-03-23-glitchtip-6-1-released/) y [artículo de abril](https://glitchtip.com/blog/2026-04-20-duckdb-parquet-archives/). No confundir almacenamiento directo en Parquet con el tránsito caliente/frío de logs. |
| Transacciones | La guía declara retención total caliente más fría para transacciones. El anuncio 6.1 permite transacciones sin DuckDB y el 6.2 añade visualización de spans en su detalle. | [Instalación](https://glitchtip.com/documentation/install/) y [anuncio 6.2](https://glitchtip.com/blog/2026-06-22-glitchtip-6-2-released/). Las páginas no precisan aquí todas las reglas de archivo de transacciones individuales. |

La [configuración de instalación](https://glitchtip.com/documentation/install/)
separa retención y umbral caliente por tipo, activa archivo opcional y permite
almacenamiento local o S3. También describe limpieza de archivos vencidos.
El resultado de producto es conservar historia consultable durante su plazo
y retirarla al vencer. DuckDB, Parquet o particiones no deben convertirse por
inferencia en tecnologías obligatorias para GoodSentry.

El [artículo de abril](https://glitchtip.com/blog/2026-04-20-duckdb-parquet-archives/)
es evidencia fechada de consulta que combina logs calientes y fríos. Su
explicación de arquitectura se leyó como documentación pública, sin seguir
sus enlaces a código servidor ni a bibliotecas internas.

## Incertidumbres y consecuencias para planificación

- Java: falta fijar SDK/JDK, mecanismo público de fuentes, identidad de
  asociación y resultado observable. La investigación no autoriza excluirlo.
- Archivo: debe distinguirse errores, logs, transacciones y spans al definir
  los criterios y revisar evidencia. No declarar los cuatro cubiertos por
  un comprobante limitado a spans/transacciones.
- Logs: hay evidencia documental positiva de archivo frío, no solo una duda
  por coincidencia de nombres. Falta acordar su criterio y verificar GoodSentry.
- Backend: local y S3 están documentados; qué combinaciones exigir como
  comprobantes de cierre es decisión del usuario, no de esta investigación.
- Ninguna página consultada verifica todas las reglas de consulta, permisos,
  paginación, correlación o eliminación al mezclar datos calientes y fríos.
  Esas reglas deberán precisarse en el contrato y demostrarse en su alcance.

## Límites de la referencia y método

Los anuncios 6.1 y el artículo de abril son anteriores a la referencia
v6.2.6 aprobada, pero no son una prueba de cada parche. Las guías actuales
son rolling y pueden haber cambiado. Este informe conserva URLs y fecha,
no congela su contenido ni acredita ejecución de una instancia v6.2.6.
No se modificó la referencia, el contrato canónico, el plan, los tickets o
el Goal. No se ejecutaron builds, tests, despliegues ni aceptación.
No se consultó implementación del servidor Sentry, Relay o GlitchTip,
pruebas, esquemas de base de datos ni assets. Una búsqueda adicional en
Sentry Docs no aportó un flujo Java verificable; no se usa para afirmar
compatibilidad de GlitchTip.
