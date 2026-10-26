---
id: WF-0006
title: Acordar las pruebas externas y sus dependencias
status: closed
labels: [wayfinder:grilling]
parent: WF-0001
assignee: jerolan
claim_session: null
blocked_by: [WF-0004]
---

## Question

¿Qué destinos y proveedores representativos se verificarán externamente
antes del cierre, y qué acceso o intervención del usuario necesita cada prueba?

Conservar la decisión de que una integración anunciada como verificada
requiere un comprobante externo real. Definir resultado esperado, evidencia
conservable sin secretos, responsable y tratamiento de una dependencia ausente.
Una prueba local o un test omitido no sustituye ese comprobante.
No pedir credenciales por chat ni enviar mensajes externos en esta planificación.

## Inputs from the compatibility decision

[Los flujos aprobados](0004-contrato-compatibilidad.md) incluyen consultas desde
Grafana y GitLab Error Tracking como consumidores externos de la API.
Antes del contrato final, fijar aquí versión/edición exactas de GitLab,
instancia, acceso y comprobante conservable del flujo ya acordado. Conservar
las versiones Grafana/plugin aprobadas; no sustituir recibos externos por
pruebas locales. La selección de proveedores sociales y destinos de alertas
quedó abierta al cerrar aquella decisión. La resolución siguiente fija el
alcance vigente; los pendientes históricos no prevalecen sobre ella.

## Working discussion

Respuesta directa del usuario en la sesión
`codex-01a17693-32b5-7001-b461-39b34f2177d7`:

> Si, yo creo que alerta un webhook genérico y que el consumidor por ahora mapee
>
> No buscaría inicio de sesión con proveedores
>
> No buscaría confirmar la prueba levantando gitlab local

Acuerdos y límites de esta respuesta:

- Para alertas, seleccionar un webhook genérico y dejar el mapeo específico
  de cada destino al consumidor. No se aprueban por inferencia recibos SMTP
  ni recibos de canales específicos. Falta concretar receptor, resultado y
  evidencia, y confirmar cómo queda SMTP respecto al criterio anterior.
- El usuario no busca verificar inicio de sesión con proveedores en este
  cierre. Esto cambia el criterio de comprobación externa previamente
  propuesto; conservar las capacidades y controles existentes. Falta
  confirmar el alcance de la excepción en el contrato final de esta decisión.
- No levantar GitLab local para confirmar su integración. La respuesta no
  permite concluir si se acepta una instancia alojada o si se aplaza la
  comprobación real previamente aprobada. Pregunta pendiente al usuario.

No se cierra la decisión ni se modifica el producto, sus documentos
canónicos o un Goal. Las propuestas anteriores del agente no aprobadas
no constituyen obligaciones nuevas.

## Resolution comments, GitLab deferral

El usuario respondió "Si" a la recomendación explícita de aplazar la
comprobación real de GitLab para este cierre, documentarla como pendiente
y no anunciarla como verificada. Esta aprobación sustituye la exigencia
anterior de un comprobante real de GitLab Error Tracking como condición de
cierre, como excepción expresa. No exige levantar una instancia local ni
buscar una alojada. Conservar el soporte y las regresiones existentes.
No se fijan versión, edición o accesos de una prueba aplazada. La excepción
no se extiende a Grafana ni a otras integraciones.

Evidencia: respuesta directa del usuario en esta sesión a la pregunta sobre
GitLab. La decisión global permanece abierta para concretar los demás
comprobantes externos. No se modifica el producto ni un Goal.

## Resolution comments, final resolution

2026-10-25, America/Mexico_City. Sesión
`codex-01a17693-32b5-7001-b461-39b34f2177d7`.
El usuario respondió "Si" a los dos criterios concretos propuestos para
completar esta decisión: webhook genérico y exclusión de comprobantes de
proveedores externos. Se cierra con los acuerdos siguientes.

### Webhook genérico

Provocar una alerta y comprobar que un receptor HTTP independiente de
GoodSentry recibe el JSON correcto. Verificar reintentos ante fallos y
conservar evidencia sin secretos. El consumidor es responsable del mapeo
hacia su destino. No se exige elegir ni validar un formato o canal de Slack,
Discord, Teams u otro proveedor específico.

Esta aceptación demuestra el webhook genérico. Un receptor local o de
pruebas no constituye un comprobante de un proveedor externo y no autoriza
anunciar sus canales como verificados. Se conserva el criterio aprobado de
reglas de frecuencia y transiciones de uptime, SMTP/HTTP local, filtros,
cooldown, historial y recuperación tras fallos/reinicio. No se reduce a
comprobar que una petición aislada devuelve éxito.

### Excepciones expresas para este cierre

No exigir comprobantes reales de canales de alertas, proveedor de correo o
inicio de sesión externo. Conservar las funciones existentes y sus pruebas
locales, incluidos los controles de SMTP, OAuth/OIDC y proveedores sociales
según sus contratos actuales. El acuerdo excluye sus recibos externos como
condición de cierre, no autoriza eliminar funciones o controles.

Se mantiene el aplazamiento de la comprobación real de GitLab registrado
arriba. No levantar GitLab local ni exigir una instancia alojada, versión,
edición o accesos para una prueba aplazada. Conservar soporte y regresiones.
GitLab y los proveedores sin comprobante no se anunciarán como verificados.
Estas son excepciones expresas a los requisitos anteriores de comprobación
externa. No convierten pruebas sintéticas en comprobantes reales.

### Grafana y evidencia

Grafana conserva el flujo aprobado: Grafana 13.0.2 con Sentry datasource
2.2.8, conexión por token, consulta de issues y serie temporal de errores,
con alcance y revocación del token. Es un consumidor independiente, no una
dependencia de runtime. Revisar la evidencia existente y sus invalidaciones
antes de reutilizarla; si no cubre la revisión aceptada, obtener un nuevo
comprobante. No declarar vigente la evidencia solamente por estar registrada.

Para aplicar estos criterios al ejecutar el contrato, el agente ejecutor
seleccionará y configurará el receptor de pruebas y el entorno Grafana;
conservará revisión probada, versiones aplicables, resultado esperado y
observado y comprobante del receptor o consumidor. Para el webhook, incluir
la correlación entre alerta, payload recibido e intentos de entrega. Excluir
credenciales, tokens y datos personales innecesarios. Son reglas para el
registro de evidencia, no pruebas ejecutadas en esta sesión.

Si falta acceso o intervención necesaria para una prueba exigible, registrar
el bloqueo concreto y solicitar únicamente esa intervención al usuario por
un medio seguro. No pedir credenciales por chat, contratar servicios por
inferencia, omitir silenciosamente la prueba ni contar el bloqueo como éxito.
Las pruebas aplazadas anteriores no se convierten por ello en bloqueos.

Las demás capacidades, incluido archivo local y compatible S3, conservan
sus criterios en la decisión de flujos y los contratos existentes; estas
excepciones no se extienden a ellas ni al diagnóstico nativo aprobado.

El detalle de esta resolución tiene autoridad sobre los estados históricos
de discusión en este archivo. Evidencia de aprobación: respuestas directas
del usuario a la propuesta de aplazar GitLab y a los dos criterios finales.
No se implementa ni ejecuta aceptación, no se integran todavía cambios en
SPEC.md, tasks/plan.md o tasks/todo.md y no se modifica ningún Goal.
La integración canónica y el contrato propuesto corresponden a
[Aprobar el contrato de cierre y su Goal reanudable](0007-goal-reanudable.md).
