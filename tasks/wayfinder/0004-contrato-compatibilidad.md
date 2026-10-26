---
id: WF-0004
title: Acordar los flujos que prueban equivalencia con GlitchTip
status: open
labels: [wayfinder:grilling]
parent: WF-0001
assignee: jerolan
claim_session: 01a17668-8461-7c71-bb8a-c64f02f890be
blocked_by: [WF-0002, WF-0003]
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

## Next round, native acceptance

Propuesta pendiente: usar Linux/amd64 con sentry-native 0.17.4 y Crashpad
como plataforma representativa exigida para la prueba nativa real completa.
El crash debe proceder del mismo ELF/DWARF que se subió, producir un issue
con función goodsentry_demo_crash, archivo main.c y línea positiva, y no
conservar los bytes del minidump. La investigación y tasks/todo.md registran
una ejecución real de ese flujo; su reutilización depende de la revisión
acreditada y de los cambios que puedan invalidarla.

Se propone conservar la cobertura y regresiones de los formatos y
arquitecturas ya soportados, sin exigir un crash real adicional por cada
combinación como condición de cierre. Esta última limitación necesita
aprobación expresa, pues hay pendientes históricos de comprobantes en varias
arquitecturas. No se declaran resueltos por aprobar el demo representativo
los otros pendientes nativos de CFI, archivos o servidores de símbolos.
Contexto Java sigue como una incertidumbre distinta.
