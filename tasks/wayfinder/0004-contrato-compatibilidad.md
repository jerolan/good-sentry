---
id: WF-0004
title: Acordar los flujos que prueban equivalencia con GlitchTip
status: closed
labels: [wayfinder:grilling]
parent: WF-0001
assignee: jerolan
claim_session: null
blocked_by: [WF-0002, WF-0003, WF-0008, WF-0009]
---

## Question

¿Qué conjunto finito de capacidades, clientes, versiones y resultados
observables constituye la equivalencia funcional mínima de GoodSentry con
la referencia propuesta de GlitchTip?

El usuario acepta equivalencia funcional, no reproducción interna completa.
Proponer criterios identificables y verificables, y reconciliarlos con el
alcance ya aprobado. El usuario acepta la referencia y las diferencias;
una inferencia del agente no sustituye esa decisión.
## Working discussion

25 de octubre de 2026, America/Mexico_City. Sesion
01a17668-8461-7c71-bb8a-c64f02f890be. Decisión abierta, con aprobación parcial de las dos primeras rondas.

Se reutilizan [la referencia candidata](research/glitchtip-baseline.md) y
[la evidencia del producto](research/goodsentry-evidence.md). Las dos
investigaciones estan cerradas; sus propuestas no equivalen a aprobacion.

Primera ronda aprobada por el usuario: adoptar GlitchTip v6.2.6, tag
`d3039088e913a591a8e6544a2d2fffc865a9711a`, como referencia de producto,
congelando una lista explicita de resultados observables y sus fuentes.
Las paginas rolling no agregaran obligaciones futuras automaticamente.
Esta propuesta conserva los requisitos ya aprobados en SPEC.md; cualquier
cambio de alcance necesita una decision expresa. No se propone aprobar aun
una matriz completa, versiones de clientes ni exclusiones.

Incertidumbres para las siguientes rondas: precedencia entre anuncio 6.2 y
guia de logs sobre OTLP directo; distincion entre archivo frio de logs y de
spans; alcance de diagnostico nativo y contexto Java; clientes, versiones y
resultados exigibles por capacidad. La comparacion debera reconciliar cada
criterio con SPEC.md sin convertir brechas de evidencia en sacrificios.
Los destinos y comprobantes externos se resolveran en su ticket dependiente.

No se modifican SPEC.md, el plan, las tareas de implementacion ni el Goal.
La reclamacion permanece vigente mientras esta sesion trabaja la decision.

## Resolution comments

25 de octubre de 2026, America/Mexico_City. Resolución parcial.
El usuario respondió "1 si" a la propuesta de referencia fija.
Se aprueba GlitchTip v6.2.6 con la identidad registrada arriba, una lista finita
de resultados observables sustentados por las fuentes registradas, sin ampliar
obligaciones automáticamente por cambios futuros de documentación y conservando
los requisitos ya aprobados de GoodSentry. Cualquier reducción requiere una
decisión expresa. Evidencia: respuesta directa del usuario en esta sesión.
No se aprueban todavía clientes, versiones, flujos completos ni exclusiones.
La issue permanece abierta hasta resolver el conjunto de su pregunta.

## Coverage approved in round 2

El usuario aprobó usar como familias del mínimo funcional
las once áreas de la matriz investigada: errores y DSN; alertas; source maps;
rendimiento; logs; uptime; diagnóstico nativo; MCP; integraciones de lectura;
identidad y administración; operación y retención. Cada familia deberá tener
criterios explícitos con resultado observable y evidencia exigible.
Respuesta directa del usuario: "Si". La lista establece cobertura por familia;
sus clientes, versiones, amplitud y casos límite se concretarán en rondas posteriores. No declara implementada
ninguna capacidad ni excluye requisitos existentes.

OTLP directo ya tiene contrato explícito en SPEC.md y se conserva. La
contradicción de las fuentes no elimina ese requisito. Sigue pendiente
precisar qué puede afirmarse de la referencia, sin equiparar autenticación
propia de GoodSentry con comportamiento verificado de GlitchTip.
Archivo frío de logs frente a spans y contexto Java siguen sin resolución.

## Resolution comments, round 2

25 de octubre de 2026, America/Mexico_City. Resolución parcial.
El usuario aprobó las once áreas propuestas como mínimo funcional de cierre,
con los resultados observables presentados: captura y consulta de errores;
reglas y entrega de alertas; stacks legibles mediante source maps; consulta de
transacciones, spans y estadísticas; ingestión, búsqueda y correlación de logs;
checks y heartbeats con fallo y recuperación; procesamiento de crashes y
resolución de símbolos; consulta y operación mediante MCP; lectura desde
GitLab Error Tracking y Grafana; acceso y gestión de organizaciones, equipos y
proyectos; despliegue, persistencia, retención y archivo.
Se conservan los requisitos existentes. Contexto Java y archivo frío de logs
siguen pendientes, junto con los flujos, clientes y versiones concretos.
La issue continúa abierta. No se aprueba implementación ni se crea un Goal.

## Client versions approved in round 3

El usuario aprobó fijar los clientes ya documentados como
base de aceptación, evitando que versiones futuras amplíen automáticamente
el cierre: sentry-go 0.50.0; sentry-sdk Python 2.72.0; Node y Browser 11.7.0;
sentry-cli 3.8.0; Sentry Native 0.17.4; exportador Python OTLP protobuf/HTTP
1.45.1 y JSON/HTTP 0.66b1; Grafana 13.0.2 con Sentry datasource 2.2.8.

Fuentes: docs/compatibility.md, investigación goodsentry-evidence.md y
proveniencia OTLP de SPEC.md. Se propone reutilizar evidencia únicamente en
su alcance y revisión acreditados, sin declarar que toda prueba existente
sigue vigente sobre cambios posteriores. Esta selección no aprueba todavía
la matriz de plataformas nativas, todos los flujos ni cobertura universal.
Versiones de cliente MCP, GitLab y cliente Java siguen por concretar.

## Resolution comments, round 3

25 de octubre de 2026, America/Mexico_City. Resolución parcial.
El usuario respondió "Si, por que hay grafana?" a las versiones propuestas.
Se aprueban las versiones enumeradas arriba. Su pregunta pide explicar el
papel de Grafana y no expresa una exclusión ni retira la cobertura aprobada.

Grafana es un consumidor externo de la API compatible, mediante el plugin
Sentry datasource. La documentación pública de integraciones de GlitchTip
lo incluye junto con GitLab Error Tracking. Se volvió a consultar
https://glitchtip.com/documentation/integrations/ para confirmar el motivo;
esta consulta no modifica la referencia congelada ni amplía su alcance.
Grafana no pasa a ser una dependencia de ejecución de GoodSentry. El producto
conserva su UI propia y PostgreSQL como único servicio externo requerido.

Evidencia registrada en docs/compatibility.md: Grafana 13.0.2 y plugin 2.2.8
se ejercitaron para conexión a organización/proyecto, consulta de issues y
panel de conteos horarios sobre /stats_v2/. Esta lectura confirma el registro,
no reproduce esa prueba ni certifica su vigencia en revisiones posteriores.

Siguen pendientes clientes de MCP, GitLab y Java, plataformas nativas y
resultados completos por familia. La decisión permanece abierta.

## Resolution comments, Grafana clarification

2026-10-25, America/Mexico_City.
Tras aclarar que GoodSentry expone endpoints compatibles consultados por el
plugin Sentry de un Grafana externo, el usuario respondió "Ok bien entonces si".
Queda confirmado ese alcance de la integración de lectura y las versiones
aprobadas. Grafana no se incorpora al producto ni a sus dependencias de runtime.

## Native acceptance approved in round 4

El usuario aprobó usar Linux/amd64 con sentry-native 0.17.4 y Crashpad
como plataforma representativa exigida para la prueba nativa real completa.
El crash debe proceder del mismo ELF/DWARF que se subió, producir un issue
con función goodsentry_demo_crash, archivo main.c y línea positiva, y no
conservar los bytes del minidump. La investigación y tasks/todo.md registran
una ejecución real de ese flujo; su reutilización depende de la revisión
acreditada y de los cambios que puedan invalidarla.

Se conserva la cobertura y regresiones de los formatos y
arquitecturas ya soportados, sin exigir un crash real adicional por cada
combinación como condición de cierre. El usuario aprobó expresamente esa limitación de los comprobantes reales;
los pendientes históricos de recibos por arquitectura deberán reconciliarse
con este acuerdo al preparar el contrato final. No se declaran resueltos por aprobar el demo representativo
los otros pendientes nativos de CFI, archivos o servidores de símbolos.
Contexto Java sigue como una incertidumbre distinta.

## Resolution comments, round 4

2026-10-25, America/Mexico_City. Resolución parcial.
El usuario respondió "Si" a Linux/amd64 como plataforma representativa de la
prueba nativa real, con sentry-native 0.17.4 y Crashpad, símbolos del mismo
ELF, función/archivo/línea y descarte del dump. Se aprueba no exigir otro
crash real por cada combinación para cerrar, conservando cobertura y
regresiones de los formatos y arquitecturas soportados. Los demás pendientes
nativos siguen sujetos a revisión. Evidencia: aprobación directa en sesión.

## MCP client approved in round 5

El usuario aprobó fijar como cliente de aceptación MCP el cliente oficial
Go modelcontextprotocol/go-sdk v1.8.0, ya fijado en go.mod y SPEC.md y usado
como mcp.NewClient en internal/server/mcp_test.go. Ejercitar el endpoint real
por Streamable HTTP, descubrir e invocar las 17 herramientas acordadas y
verificar los resultados contra datos de GoodSentry, conservando las pruebas
de autorización y del ciclo OAuth/token exigidas por el contrato existente.
No exigir una matriz de todas las aplicaciones consumidoras MCP como
condición de cierre. La selección del cliente no declara vigentes todas las
pruebas ni elimina los requisitos de OAuth. No se ejecutan pruebas aquí.
GitLab, contexto Java, archivo frío y los otros flujos siguen pendientes.

## Resolution comments, round 5

2026-10-25, America/Mexico_City. Resolución parcial.
El usuario respondió "Si" al cliente oficial MCP Go SDK 1.8.0 y al criterio
de conectar contra el endpoint real, descubrir e invocar las 17 herramientas,
comprobar resultados y verificar permisos, token y OAuth. No se exige una
matriz de todas las aplicaciones consumidoras MCP para cerrar.
Evidencia: aprobación directa del usuario en esta sesión. La decisión global
permanece abierta y se mantienen los requisitos existentes.

La precisión factual de contexto Java y archivo frío se sigue en
[Precisar contexto Java y archivo frío de la referencia](0008-contexto-java-archivo-frio.md).

## Errors and JavaScript source maps approved in rounds 6 and 7

Dos criterios independientes aprobados por el usuario:

- Errores y triage: para cada SDK Go/Python/Node/Browser fijado, capturar
  un error real por DSN y recuperarlo mediante API y UI con su contexto,
  tags, entorno y release. Comprobar agrupación y ausencia de duplicación
  ante reintento del mismo evento. Demostrar búsqueda/filtros y acciones
  de triage ya contratadas sobre esos datos; verificar persistencia tras
  reinicio. No basta responder éxito a la ingestión.
- Source maps JavaScript: usar sentry-cli 3.8.0 para preparar/subir los
  artefactos del código minificado probado, emitir errores reales desde
  Node y Browser 11.7.0 y comprobar que el issue muestra función, archivo,
  línea y contexto del código original. Mantener trazabilidad al artefacto
  correcto, persistencia tras reinicio y los mecanismos de selección por
  Debug ID o release/distribution ya contratados. Contexto Java se investiga
  por separado y esta propuesta no lo excluye.

Estos criterios concretan evidencia de cierre sin sustituir ni reducir los
contratos existentes de SPEC.md. Destinos externos de alertas e identidad
siguen en su ticket de decisión propio.

## Resolution comments, rounds 6 and 7

2026-10-25, America/Mexico_City. Resolución parcial.
El usuario respondió "Si" a ambos criterios de errores/triage y source maps
JavaScript propuestos. Se aprueban los resultados y clientes descritos arriba,
incluida persistencia tras reinicio. Evidencia: respuesta directa en sesión.
No se declara ejecutada ni vigente una prueba por registrar su criterio.

## Archive approved in round 8 and original Java proposal superseded

Archivo aprobado; la propuesta Java de esta ronda fue sustituida por su
aplazamiento explícito registrado más abajo.

- Archivo y retención: distinguir errores, logs, transacciones y spans.
  Durante la retención acordada, los datos archivados deben poder consultarse
  por los lectores aplicables de UI/API, con filtros y permisos coherentes
  entre datos recientes y archivados, y sobrevivir a reinicios. Verificar
  eliminación al vencer el plazo. Exigir backend local y otro compatible S3,
  sin imponer DuckDB como tecnología interna de GoodSentry. Los tipos pueden
  tener estrategias diferentes; no se exige mover spans desde PostgreSQL
  si ya se almacenan directamente en el archivo. Aprobado en la ronda 8.
- Contexto Java: exigir un error real de un cliente Java que muestre función,
  archivo, línea y el fragmento de código fuente correspondiente en el issue.
  El crash nativo y los source maps JavaScript no demuestran este resultado.
  Es un criterio propuesto de GoodSentry, no un detalle comprobado de GlitchTip.
  SDK/JDK, artefacto y mecanismo de asociación siguen pendientes de investigación
  y fijación antes de aprobar el contrato final. Pendiente de aprobación.

La investigación factual distingue errores/logs archivados y spans en Parquet.
No hay aún comprobante de equivalencia de GoodSentry para cada tipo. No se
infiere implementación, ausencia de soporte ni una exclusión aprobada.

## Discussion, conditional Java scope

2026-10-25, America/Mexico_City. El usuario expresó que Java parece comenzar
a exceder el alcance y condicionó su inclusión a que sea una capacidad
buscada/fundamental y una pieza trivial o conocida. Esto no constituye una
aprobación incondicional del criterio Java propuesto ni una exclusión.
Se investiga [utilidad y esfuerzo del contexto Java](0009-viabilidad-contexto-java.md)
antes de recomendar una decisión. La presencia en el anuncio de GlitchTip
no prueba por sí sola necesidad, demanda o bajo esfuerzo en GoodSentry.
La propuesta de archivo y retención queda pendiente de respuesta inequívoca;
no se atribuye aprobación al "Ok" que precede a la objeción sobre Java.

## Java source-context exception approved

La investigación revisada no satisface la condición de bajo esfuerzo o
fundamentalidad expresada por el usuario. Se aprueba aplazar el enriquecimiento
automático con fuentes Java como excepción explícita al mínimo documentado de
GlitchTip, manteniendo los requisitos de errores ya aprobados y sin declarar
probado un SDK Java. No se elimina código existente. La propuesta anterior
de exigir contexto Java queda sustituida por este aplazamiento.
El criterio de archivo frío de la ronda 8 fue aprobado posteriormente; ver su resolución.

## Resolution comments, Java deferral

2026-10-25, America/Mexico_City. Resolución parcial.
El usuario respondió "Si" a la recomendación explícita de aplazar el
enriquecimiento con fuentes Java como excepción al mínimo de GlitchTip.
Se aprueba ese aplazamiento, sustentado en la investigación de
[utilidad y esfuerzo](research/java-scope-feasibility.md). La consecuencia
observable es que el cierre no exige que GoodSentry derive fragmentos de
fuentes Java desde bundles subidos. No implica que GlitchTip carezca de la
capacidad ni permite anunciar compatibilidad Java sin comprobantes.
No se amplía esta excepción a captura de errores, source maps JavaScript
ni diagnóstico nativo ya acordados. No se requiere fijar SDK/JDK o mecanismo
de bundles Java para el enriquecimiento aplazado. Cualquier aceptación básica
de cliente Java sería un flujo distinto que todavía no ha sido aprobado.
Evidencia: respuesta directa del usuario en esta sesión. La decisión de
compatibilidad global permanece abierta.

## Resolution comments, round 8

2026-10-25, America/Mexico_City. Resolución parcial.
El usuario respondió "Si" al criterio de archivo y retención retomado tras
aplazar fuentes Java. Se aprueba consultar errores, logs, transacciones y
spans archivados con filtros y permisos coherentes, conservarlos tras
reiniciar y eliminarlos al vencer su retención. Se exigen comprobantes con
almacenamiento local y compatible S3; cada tipo puede usar una estrategia
interna distinta. Evidencia: respuesta directa en sesión. No se impone
DuckDB ni se acredita aceptación ejecutada. El plazo concreto y la evidencia
por tipo deben remitir a los contratos existentes y sus comprobantes.

## Performance, logs and uptime approved in rounds 10 to 12

Tres criterios independientes aprobados por el usuario:

10. Rendimiento: usar los SDK Go/Python/Node/Browser fijados para generar
    transacciones y spans reales y comprobar en API/UI su identidad, relación
    padre/hijo, tiempos, desglose y navegación de traza. Verificar cantidades,
    errores, latencias, tendencias y detección N+1 de los contratos existentes
    contra un conjunto controlado con resultados esperados explícitos.
11. Logs: usar logs reales de los SDK fijados y los exportadores OTLP HTTP
    protobuf/JSON fijados. Verificar valores y tipos, severidad, búsqueda,
    filtros de tiempo/servicio/entorno/texto/traza y navegación a la traza
    cuando exista. Consultar los datos mediante API/UI y conservarlos tras
    reiniciar. Se mantienen los límites y semántica de cada protocolo ya
    aprobados; no se exige deduplicación donde el contrato no la promete.
12. Uptime: comprobar desde el scheduler real HEAD/GET/POST, TCP, certificado
    SSL y heartbeat, incluidos caída, recuperación, umbrales, pausa/reanudación,
    historial y estado público de los monitores seleccionados. Conservar la
    configuración y el historial tras reiniciar. Cada fallo debe observarse
    según el intervalo, timeout y umbral del monitor, sin introducir un SLA
    nuevo. Los destinos y recibos externos de avisos quedan en su ticket.

Estos criterios concretan resultados sin reducir los contratos ya aprobados.
No afirman que las pruebas existentes sigan vigentes ni autorizan ejecución.

## Resolution comments, rounds 10 to 12

2026-10-25, America/Mexico_City. Resolución parcial.
El usuario respondió "Si" a los tres criterios de rendimiento, logs y uptime.
Quedan aprobados los flujos descritos arriba con clientes fijados, resultados
esperados controlados, consulta real y persistencia. Se mantienen los contratos
existentes. Los destinos y recibos externos de alertas siguen en su ticket.
Evidencia: aprobación directa en sesión. No se ejecuta aceptación ni se da
por vigente evidencia anterior a cambios que puedan invalidarla.

## Alerts, administration and operation approved in rounds 13 to 15

Tres criterios independientes aprobados por el usuario:

13. Alertas: provocar la condición de una regla por frecuencia de errores y
    de una transición de uptime; comprobar destinatarios, entorno, umbrales,
    cooldown, generación del trabajo, entrega a receptores HTTP/SMTP reales,
    historial y reintentos recuperables tras fallo/reinicio. Estos receptores
    locales prueban el flujo interno, no sustituyen los comprobantes de los
    proveedores externos que se acordarán en su ticket dependiente.
14. Identidad y administración: comprobar en API y navegador los modos abierto
    y autenticado, alta/login/logout, recuperación de contraseña, TOTP y OIDC
    ya contratados; gestión de organizaciones, equipos, miembros, proyectos,
    DSNs y tokens. Demostrar permisos/aislamiento, expiración y revocación de
    credenciales según sus contratos actuales. Conservar los controles y las
    capacidades implementadas; esta lista de flujos no excluye otros contratos
    aprobados. Proveedores sociales y recibos externos se acordarán aparte.
15. Operación: arrancar la imagen documentada con PostgreSQL y volúmenes
    vacíos; comprobar salud/readiness, UI y captura SDK, sin requerir Valkey.
    Verificar migraciones y actualización desde una versión publicada fijada,
    persistencia tras reiniciar y restauración de un backup en un entorno
    independiente, conservando datos, artefactos y configuración necesarios.
    Las funciones opcionales deben reflejar su disponibilidad real. Mantener
    las verificaciones de imagen, concurrencia y recuperación ya exigidas,
    sin introducir promesas nuevas de memoria o rendimiento no medidos.

Se mantiene el alcance de planificación. Las revisiones/digests concretos y
los comprobantes de aceptación se fijarán al ejecutar el contrato aprobado.
No se modifica SPEC.md, el plan de implementación ni el Goal.

## Resolution comments, rounds 13 to 15

2026-10-25, America/Mexico_City. Resolución parcial.
El usuario respondió "Si" a los tres criterios de alertas, identidad y
administración, y operación. Se aprueban los resultados descritos arriba,
incluida recuperación de entregas, controles de credenciales y restauración
en un entorno independiente. Los recibos de proveedores externos conservan
su decisión propia. Evidencia: aprobación directa en esta sesión.
No se ejecuta aceptación, se publica ni se modifica el Goal.

## Releases, read integrations and browser approved in rounds 16 to 18

Tres criterios aprobados por el usuario:

16. Releases y artefactos: con sentry-cli 3.8.0, crear y finalizar una release,
    asociar metadatos locales de commits y registrar un deploy. Consultar desde
    API/UI la misma versión, proyectos, archivos y eventos asociados. Comprobar
    subida, listado, acceso y eliminación de artefactos según los contratos
    existentes, sin borrar datos ajenos a la operación; mantener permisos,
    reintentos y persistencia. Conservar los flujos de adjuntos y feedback ya
    contratados. No se introduce integración automática con repositorios.
17. Integraciones de lectura: desde un Grafana externo 13.0.2 con plugin Sentry
    2.2.8, conectar con token y verificar consulta de issues y serie temporal
    de errores. Desde un GitLab real, conectar su Error Tracking, seleccionar
    el proyecto y consultar los errores de GoodSentry. Comprobar alcance y
    revocación del token. Son consumidores externos de la API, no dependencias
    de runtime. La versión/edición exacta de GitLab, instancia y acceso se
    fijarán en Acordar las pruebas externas y sus dependencias antes de aprobar
    el contrato final; esa selección no debe ampliar los flujos aquí acordados.
18. Interfaz: recorrer los flujos acordados en un navegador real a 320, 768 y
    1440 píxeles, además de conservar las verificaciones específicas de cada
    contrato existente. Comprobar navegación con teclado, formularios y sus
    estados de carga/vacío/error/permisos, filtros persistentes y presentación
    de telemetría como texto. La UI es propia de GoodSentry; no se exige
    reproducir la apariencia de GlitchTip ni añadir un framework nuevo.

Estos criterios no declaran paridad universal de API/SDK ni reducen contratos
existentes. Esta ronda completa la decisión de flujos; el contrato final sigue pendiente.
Las selecciones externas y el contrato final siguen siendo decisiones futuras.

## Resolution comments, rounds 16 to 18 and final resolution

2026-10-25, America/Mexico_City. El usuario respondió "Si" a la pregunta
explícita de aprobar los criterios 16-18, tras aclarar GitLab Error Tracking.
Quedan aprobados releases/artefactos, integraciones de lectura y aceptación
responsive con el alcance descrito arriba. GitLab es consumidor de la API,
como Grafana; no se incorpora a la aplicación ni exige alojar el código allí.

Se cierra esta decisión con las aprobaciones registradas en sus rondas:
referencia v6.2.6 e identidad fijada; once familias funcionales; versiones de
clientes; prueba nativa representativa Linux/amd64; cliente MCP y 17 herramientas;
errores/triage; source maps JavaScript; archivo y retención; rendimiento;
logs; uptime; alertas; identidad/administración; operación; releases/artefactos;
lectores externos e interfaz. La excepción aprobada es aplazar el enriquecimiento
automático con fuentes Java. La limitación aprobada de comprobantes nativos
no exige un crash real por cada combinación y conserva las regresiones.

Se conservan los contratos existentes salvo las excepciones expresas de esta
issue. Los comentarios históricos pendientes son estados de rondas previas;
las aprobaciones posteriores y esta resolución fijan su estado actual.
No se afirma que toda la funcionalidad esté implementada o verificada. La
evidencia previa solo se reutiliza dentro de su alcance y revisión acreditados.

Las decisiones pendientes tienen dueño y no se dan por resueltas aquí:

- [Sacrificios](0005-sacrificios.md): capacidades adicionales y reconciliación de
  brechas históricas, sin reabrir las excepciones aprobadas por inferencia.
- [Pruebas externas](0006-evidencia-externa.md): proveedores/destinos, versión y
  edición exactas de GitLab, instancia y accesos, recibos y dependencias antes
  de aprobar el contrato final. El flujo GitLab de consulta ya está acordado.
- [Contrato final y continuidad](0007-goal-reanudable.md): integración canónica,
  evidencia e invalidación, protocolo de continuación y aprobación del contrato.

Se conserva la planificación. No se modifica SPEC.md, tasks/plan.md,
tasks/todo.md ni ningún Goal. No se ejecuta aceptación ni implementación.
