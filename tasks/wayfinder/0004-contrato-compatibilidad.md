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
01a17668-8461-7c71-bb8a-c64f02f890be. Decision abierta, sin respuesta del usuario.

Se reutilizan [la referencia candidata](research/glitchtip-baseline.md) y
[la evidencia del producto](research/goodsentry-evidence.md). Las dos
investigaciones estan cerradas; sus propuestas no equivalen a aprobacion.

Primera ronda pendiente: aceptar GlitchTip v6.2.6, tag
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
