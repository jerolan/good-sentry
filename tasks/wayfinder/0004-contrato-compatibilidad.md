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
01a17668-8461-7c71-bb8a-c64f02f890be. Decisión abierta, con aprobación parcial de la primera ronda.

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

## Next round

Propuesta pendiente de aprobación: usar como familias del mínimo funcional
las once áreas de la matriz investigada: errores y DSN; alertas; source maps;
rendimiento; logs; uptime; diagnóstico nativo; MCP; integraciones de lectura;
identidad y administración; operación y retención. Cada familia deberá tener
criterios explícitos con resultado observable y evidencia exigible.
La lista establece cobertura por familia; sus clientes, versiones, amplitud y
casos límite se concretarán en rondas posteriores. No declara implementada
ninguna capacidad ni excluye requisitos existentes.

OTLP directo ya tiene contrato explícito en SPEC.md y se conserva. La
contradicción de las fuentes no elimina ese requisito. Sigue pendiente
precisar qué puede afirmarse de la referencia, sin equiparar autenticación
propia de GoodSentry con comportamiento verificado de GlitchTip.
Archivo frío de logs frente a spans y contexto Java siguen sin resolución.
