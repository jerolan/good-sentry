---
id: WF-0007
title: Aprobar el contrato de cierre y su Goal reanudable
status: open
labels: [wayfinder:grilling]
parent: WF-0001
assignee: jerolan
claim_session: 01a1769a-99aa-7be2-a284-f49c32b0cb19
blocked_by: [WF-0004, WF-0005, WF-0006]
---

## Question

¿Qué contrato final y protocolo de continuación hacen que cualquier nueva
sesión conserve el mismo alcance, criterios de aceptación y punto de avance?

Proponer integración en SPEC.md, tasks/plan.md y tasks/todo.md, identificadores
estables, estados de evidencia, selección del siguiente trabajo, invalidación
de evidencia tras cambios y condiciones exactas de cierre o bloqueo.
El Goal debe remitir al contrato canónico y no duplicar toda su especificación.
Separar contador/presupuesto de cada Goal del progreso persistente del producto.

Entregar contrato y prompt concretos para revisión. Resolver con la aprobación
del usuario; integrar o ejecutar después conforme a la autorización recibida.

## Proposed closure contract, awaiting approval

2026-10-25, America/Mexico_City. Sesión
`01a1769a-99aa-7be2-a284-f49c32b0cb19`.
Propuesta para revisión humana. No es una resolución aprobada, integración
canónica, aceptación ejecutada ni autorización para iniciar o cambiar un Goal.

### Autoridades y límite de alcance

Tras aprobación y autorización de integración, incorporar este contrato en
`SPEC.md`, bajo el ancla estable `closure-contract-v1`. El diseño y los
criterios tendrán una sola autoridad allí. `tasks/plan.md` ordenará el trabajo;
`tasks/todo.md` registrará estado, evidencia y siguiente acción por criterio.
Esta decisión conservará la procedencia y aprobación, con enlace al contrato
integrado. No crear otro paquete de especificación ni un contrato paralelo.

El cierre abarca todos los requisitos aprobados de SPEC.md reconciliados con
las resoluciones de [flujos](0004-contrato-compatibilidad.md),
[sacrificios](0005-sacrificios.md) y
[pruebas externas](0006-evidencia-externa.md). Las resoluciones posteriores
prevalecen sobre estados históricos de discusión. Una discrepancia sin
resolución expresa exige una decisión, nunca una reducción silenciosa.

La referencia queda congelada en GlitchTip v6.2.6, tag
`d3039088e913a591a8e6544a2d2fffc865a9711a`. Cambios futuros de documentación,
clientes o productos no amplían el cierre. Conservar clean-room, controles,
datos y funciones existentes. Compatibilidad significa resultados observables
con los clientes fijados, sin prometer compatibilidad universal.

### Criterios identificables

Asignar identificadores inmutables `CLOSE-<familia>-<número>` a cada resultado
exigible. Las familias son errores, alertas, sourcemaps, rendimiento, logs,
uptime, nativo, mcp, lectores, identidad y operacion. Releases, artefactos,
adjuntos, feedback e interfaz conservarán criterios explícitos vinculados a
sus contratos, aunque atraviesen familias. Un ID no se renumera ni reutiliza;
una modificación semántica conserva su historial y registra la decisión.

Al integrar, enumerar todos los requisitos actuales de SPEC.md y los flujos
aprobados, vinculando cada uno con un criterio o excepción expresa. Una
familia o casilla global no sustituye sus resultados individuales. La matriz
completa debe existir antes de declarar lista la ejecución del Goal propuesto.
Las versiones de clientes y los resultados detallados aprobados se incorporan
sin ampliación ni recorte. Las pruebas que no aplican a un SDK deben justificarlo
por el contrato, no omitirse silenciosamente.

La cobertura comprende captura y triage de errores; source maps Node/Browser;
transacciones, spans, métricas y N+1; logs SDK y OTLP HTTP protobuf/JSON;
scheduler y monitores; reglas y entregas recuperables; identidad y gestión;
MCP con su cliente oficial Go y 17 herramientas; releases y artefactos;
lectores externos; operación, imagen, actualización y restauración; interfaz
real a 320/768/1440 y los controles existentes de cada contrato.

Archivo y retención conservan evidencia diferenciada para errores, logs,
transacciones y spans, almacenamiento local y compatible S3, consultas,
filtros/permisos, reinicio y vencimiento. La evidencia de spans no demuestra
por sí sola archivo de errores o logs. No se impone DuckDB.

La prueba nativa real exigida usa Linux/amd64, sentry-native 0.17.4 y Crashpad,
símbolos del mismo ELF/DWARF, función goodsentry_demo_crash, main.c y línea
positiva, con descarte de los bytes del dump. Se conservan las regresiones
de formatos y arquitecturas soportados; no se exige otro crash real por cada
combinación. Otros contratos nativos siguen exigibles si no fueron aplazados.

Las excepciones aprobadas son enriquecimiento automático con fuentes Java,
conexión automática con repositorios, WebAuthn como segundo factor y muestreo
global de eventos por organización; también los comprobantes reales de GitLab,
canales de alertas, correo y proveedores de inicio de sesión. Conservar sus
funciones y controles existentes y describir sus límites. No exigir versiones
o accesos para una prueba aplazada ni anunciarla como verificada.

Grafana 13.0.2 con Sentry datasource 2.2.8 conserva conexión por token,
issues, serie temporal, alcance y revocación. El webhook requiere receptor
HTTP independiente, JSON correcto, correlación alerta/payload/intentos y
reintentos ante fallos. Conservar los flujos HTTP/SMTP locales, reglas,
filtros, cooldown, historial y recuperación. Esa recepción no certifica un
proveedor específico. El ejecutor elegirá receptor y entorno Grafana.

### Estado y evidencia persistentes

En tasks/todo.md, cada ID enlaza al criterio de SPEC.md y registra uno de estos
estados de evidencia: pendiente, vigente, fallida, invalidada, bloqueada o
aplazada por decisión expresa. El estado de implementación se registra aparte;
una función implementada puede seguir con evidencia pendiente.

Cada comprobante registra fecha, revisión de fuente, imagen/digest cuando
aplique, versiones, entorno relevante, comando o procedimiento reproducible,
resultado esperado y observado, alcance y ruta o enlace a evidencia sin
secretos. Para pruebas externas incluye el recibo del receptor o consumidor.
Los bloqueos registran causa, dependencia, responsable de resolverla, intentos,
condición de desbloqueo y siguiente acción. Un skip no acredita éxito.

Al inicio y tras cada cambio, comparar la revisión probada y sus dependencias
con la candidata. Un cambio que afecte comportamiento, esquema, configuración,
clientes, imagen o entorno relevante invalida los comprobantes afectados.
Si no puede acotarse el efecto, dejarlos invalidados hasta comprobarlo.
Una evidencia de otra revisión solo se reutiliza con comparación documentada
que justifique su alcance vigente; la fecha o una casilla marcada no bastan.
Preservar resultados históricos, fallos y sus reparaciones.

### Selección y continuidad

Al reanudar, leer instrucciones, contrato, plan y tareas; consultar el estado
Git y el índice antes de editar. Identificar trabajo ajeno y reclamaciones
vigentes; trabajar en archivos o unidades compatibles con esa concurrencia.
No integrar cambios ajenos ni emitir aceptación final de una revisión móvil.

Actualizar la vigencia de evidencia y elegir el primer criterio exigible sin
prueba vigente cuyas dependencias estén resueltas, siguiendo el orden del plan
y, a igualdad, el ID. Registrar la elección y la siguiente acción antes de
trabajar. Si un criterio está bloqueado, avanzar en otro independiente;
no convertir un bloqueo local en bloqueo de todo el cierre.

Cada incremento deja evidencia, invalidaciones, bloqueos y siguiente acción
persistentes, y un commit atómico con solo cambios propios. Mantener el trabajo
directo en main y las condiciones de publicación ya autorizadas, sin ramas
ni PR. Contrastar las afirmaciones de otros agentes con las fuentes y checks
pertinentes antes de aceptarlas.

Ante un fallo, reproducir, investigar y corregir dentro del alcance. Tras dos
intentos distintos sin progreso comprobable sobre la misma causa, registrar
lo aprendido, evitar repetirlos sin evidencia nueva y pasar a otro criterio
independiente. Si no queda trabajo ejecutable, pedir la intervención concreta
necesaria y dejar el punto de reanudación. No inventar una exclusión.

El progreso del producto se informa por criterios y evidencia, sin porcentaje
inferido de casillas. Los tokens, tiempo y presupuesto pertenecen a cada Goal;
su agotamiento no completa el producto ni borra avances. Una sesión nueva
retoma el registro, no reinicia aceptación ni reconstruye todo el historial.

### Cierre y bloqueo

El producto cierra únicamente sobre una revisión candidata identificada y
una imagen vinculada a ella: todos los requisitos están trazados; cada criterio
exigible tiene evidencia vigente y satisfactoria; las únicas excepciones son
expresas; no quedan fallos ni bloqueos exigibles; las limitaciones y afirmaciones
públicas coinciden con la evidencia. Pasan los checks requeridos por SPEC.md,
incluidos tests, race, vet, integración PostgreSQL, navegador, SDKs, imagen,
recuperación/concurrencia, y la aceptación y publicación ya contratadas.
Revisar invalidaciones una última vez y registrar la auditoría por requisito.
Si cambia la candidata durante esa auditoría, reevaluar los comprobantes afectados.

Un bloqueo o evidencia pendiente impide declarar éxito. Si no queda trabajo
independiente, registrar el bloqueo real y la intervención o cambio externo
que permitiría continuar. El estado técnico del Goal seguirá las reglas del
host: marcar blocked solo cuando la misma condición impida progreso durante
tres turnos consecutivos del Goal, contando el inicial. Pausar requiere petición
expresa del usuario; completar requiere haber satisfecho el contrato entero.
Un límite de presupuesto conserva la tarea incompleta y su punto de reanudación.

### Integración y puesta en marcha

La aprobación de esta propuesta resolverá esta decisión y actualizará el índice
del mapa. No implica iniciar implementación ni cambiar el Goal activo.
Integrar luego en SPEC.md, plan y tareas según autorización vigente, preservando
cambios concurrentes. Verificar trazabilidad completa y enlaces antes de usar
el prompt siguiente. Crear o reanudar el Goal en la sesión elegida requiere
instrucción expresa del usuario. No sustituir el Goal de otra sesión.

### Prompt propuesto para el Goal futuro

> Completa el contrato de cierre de GoodSentry definido en
> /home/dev/projects/SentryGo/SPEC.md#closure-contract-v1. Usa
> tasks/plan.md para el orden y tasks/todo.md para el avance persistente.
> Antes de actuar, confirma que el contrato está aprobado e integrado con
> todos sus criterios trazados; si falta esa integración, informa la dependencia
> y solicita la instrucción necesaria para completarla.
> Reanuda desde el primer criterio exigible sin evidencia vigente y con
> dependencias resueltas, aplicando las reglas del contrato para selección,
> invalidación, concurrencia, comprobantes, excepciones y continuidad.
> Implementa y verifica los incrementos necesarios dentro de su alcance y de
> las autorizaciones vigentes. Conserva el registro después de cada incremento
> y deja siempre una siguiente acción concreta. Declara complete únicamente
> cuando la auditoría final satisfaga todas las condiciones del contrato;
> trata los bloqueos según sus reglas y las del host. El presupuesto de este
> Goal limita esta ejecución, no define el progreso ni el cierre del producto.

No se propone aquí un presupuesto numérico ni se crea, reactiva o modifica
ningún Goal. La aprobación humana de esta propuesta sigue pendiente.
