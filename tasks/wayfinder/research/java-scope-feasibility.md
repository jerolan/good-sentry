# Contexto Java: utilidad y distancia de implementación

Investigación factual acotada para la condición expresada por el usuario sobre
Java. Consulta: 2026-10-25, America/Mexico_City. GoodSentry observado en
`21dfb149bc9ecbd17376fbd1582a4d75587a6b15`. No es una decisión de alcance ni
una estimación de implementación. El usuario no aprobó incondicionalmente Java.

## Resultado

El contexto de código Java es una capacidad conocida y publicada desde 2023,
no una invención de esta planificación. Enriquece el stack con el código de
alrededor de una línea. La captura normal de una excepción y su stack es un
flujo distinto que no requiere subir ese código. Las fuentes consultadas no
prueban que el enriquecimiento sea fundamental para los usuarios de GoodSentry
ni cuantifican demanda.

GoodSentry tiene reutilizables la conservación del payload, la representación
de contexto en UI y almacenamiento de debug files con debug ID. No se encontró
un camino productivo de asociación y enriquecimiento de fuentes JVM. Al menos
hay piezas nuevas entre el bundle subido y los frames del evento. No hay
sustento para presentarlo como un arreglo trivial o como algo ya implementado.

## Fuentes públicas y flujo documentado

- El [anuncio Sentry del 19 de junio de 2023](https://sentry.io/changelog/2023-6-19-link-stack-traces-to-source-code-in-java/)
  presenta contexto Java para Android, backend y desktop, con plugins Gradle y
  Maven o configuración manual mediante sentry-cli. Es evidencia de una feature
  establecida y su utilidad, no una medición de popularidad ni una necesidad
  universal para capturar errores.
- El [anuncio de bundles JVM, del 30 de abril de 2026](https://sentry.io/changelog/multi-module-jvm-source-bundles-in-sentry-cli/)
  documenta `sentry-cli debug-files bundle-jvm --output ./out --debug-id YOUR_UUID
  ./path/to/root`. Desde CLI 3.4.1 acepta raíz de proyecto/módulo y recoge fuentes
  Java y otros lenguajes JVM. Documenta exclusiones y colisiones de nombres entre
  variantes. Esto aporta un comando público preciso que faltaba en la primera
  investigación. CLI 3.8.0, ya aprobado, es candidato compatible por versión;
  no se ejecutó ni verificó aquí su tráfico o formato exacto.
- El [artículo oficial de soporte sobre verificación de contexto Java](https://www.sentry.help/en/articles/13965165-java-how-do-i-verify-my-events-are-set-up-properly-for-source-context)
  requiere comprobar un debug ID en `debug_meta` del evento y un bundle subido
  al mismo proyecto con ese ID. Cubre plugins Java y subida manual con CLI.
  La asociación no se satisface solo almacenando un archivo de fuentes.
- El [anuncio GlitchTip 6.1](https://glitchtip.com/blog/2026-03-23-glitchtip-6-1-released/)
  anuncia contexto Java pero no especifica el flujo completo. Estos documentos
  Sentry explican clientes públicos; no prueban la implementación de GlitchTip
  v6.2.6. Se conserva esa distinción.

Las páginas `docs.sentry.io/cli/dif/` y la guía source-context enlazada no fueron
legibles por la herramienta web en esta consulta. No se rellenaron sus detalles
por suposición ni se leyó implementación del servidor.

## Qué existe en GoodSentry, con referencias

Rutas relativas a `/home/dev/projects/SentryGo`.

| Pieza | Evidencia estática | Límite |
|---|---|---|
| Conservación del evento | `internal/event/event.go:90` declara conservar todos los campos; `:171-184` serializa el mapa de campos, reemplazando event_id, a Event.Data. | Demuestra el camino genérico por lectura, no aceptación con un SDK Java real. |
| Proyección de excepciones | `internal/eventview/events.go:89` obtiene la colección de exception y `:136` la expone en entries; pruebas existentes de contexto en `internal/eventview/events_test.go:29-89`. | La prueba observada usa datos JavaScript; no acredita cliente Java. |
| UI de contexto fuente | `internal/server/web/app.js:2750-2766`, stackFrame, representa context_line, pre_context y post_context con números de línea. | Funciona cuando esos datos ya están en el frame; no los deriva de un bundle Java. |
| Contexto generado por source maps | `internal/artifact/sourcemap.go:259-261` añade los tres campos; `internal/server/sourcemaps.go:132-171` busca source maps por release/dist o imágenes de tipo sourcemap. | No se observó asociación de imágenes JVM ni lector de bundles JVM. |
| Almacén de debug files | `internal/store/debug_files.go:88-131` conserva bytes, tipo y debug ID por proyecto, deduplicando SHA-1; `internal/server/debug_files.go:140-200` ensambla chunks y guarda metadata. | Almacenar bytes o metadata no equivale a interpretar el manifest y aplicar contexto. El upload real de un bundle JVM no fue probado. |
| Parseo de símbolos nativos | `internal/native/debug_objects.go:245-270` contempla SymCache, PDB, object symbols/unwind y Breakpad. Los ZIP se recorren como contenedores de objetos. | No hay en esa ruta una interpretación de source bundle JVM/manifest y asociación de fuentes. |
| Reprocesamiento | `internal/native/reprocessing.go:73-87` requiere platform native y módulos nativos para resimbolicar. | No sirve como comprobante ni ruta Java existente. |

La búsqueda productiva enfocada de `sourcebundle`, `source_bundle`, `bundle-jvm`,
`.jvm` y `jvm` en `internal/` no encontró coincidencias. Es evidencia negativa
acotada, reforzada por los caminos leídos; no sustituye una ejecución Java.

## Delta mínimo demostrado y desconocidos

Para un camino limitado de contexto Java con bundle subido, faltaría como mínimo:

1. Caracterizar con el cliente fijado el bundle/manifest y el evento Java con su
   debug ID. Acordar SDK/JDK exactos y un ejemplo sin ofuscación antes de generalizar.
2. Interpretar y validar el bundle para seleccionar sus fuentes por identidad,
   nombre JVM y línea. Mantener los límites y aislamiento de proyecto existentes.
3. Buscar el bundle correcto desde debug_meta y enriquecer los frames Java con
   context_line/pre_context/post_context antes de las lecturas pertinentes.
4. Probar CLI real, excepción SDK Java real, recuperación API/UI y persistencia;
   además, IDs incorrectos, bundle ausente, rutas ambiguas y aislamiento. Los
   dueños actuales son `internal/server`, `internal/store`, `internal/artifact`
   o un nuevo módulo específico, `internal/eventview` y su aceptación externa.
   La ubicación final es decisión de diseño del primario, no de este informe.

La UI y parte del almacenamiento podrían reutilizarse sin cambios. No se
concluye si hace falta migración, cuántos archivos cambiarían ni cuánto tardaría.
El soporte de ProGuard/R8, Kotlin/Scala, múltiples variantes, integración con
repositorios y búsqueda remota de fuentes son problemas distintos; esta
investigación no los propone como requisitos de cierre.

No se demostró un bug conocido de GoodSentry que baste corregir localmente.
El resultado observado es una capacidad de enriquecimiento ausente en los
caminos inspeccionados, con contratos cliente conocidos y reutilización parcial.

## Método y límites

Se reutilizó `java-and-cold-archive.md` y el ticket de compatibilidad; se hicieron
búsquedas y lecturas enfocadas de GoodSentry. No se consultó implementación
servidor de Sentry/Relay/GlitchTip, sus pruebas, esquema ni assets. No se realizó
una auditoría amplia de popularidad, no se ejecutaron builds/tests/clientes y no
se modificó producto, contrato, tickets o Goal. Solo se crea este informe.
