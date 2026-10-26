---
id: WF-0006
title: Acordar las pruebas externas y sus dependencias
status: open
labels: [wayfinder:grilling]
parent: WF-0001
assignee: null
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
sigue abierta. Este ticket continúa sin reclamar hasta su sesión de trabajo.
