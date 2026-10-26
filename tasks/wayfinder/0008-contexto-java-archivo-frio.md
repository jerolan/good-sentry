---
id: WF-0008
title: Precisar contexto Java y archivo frío de la referencia
status: closed
labels: [wayfinder:research]
parent: WF-0001
assignee: jerolan
claim_session: null
blocked_by: [WF-0002, WF-0003]
---

## Question

¿Qué resultados observables documenta la referencia de GlitchTip para
contexto de código fuente Java y consulta de datos archivados en frío?
Distinguir Java de minidumps, identificar clientes/CLI públicos y precisar
si el archivo frío comprende logs, spans o transacciones. Señalar límites
rolling y evidencia faltante sin decidir exclusiones ni leer implementación
del servidor. Reutilizar la investigación previa.

Evidencia esperada en research/java-and-cold-archive.md. Investigación
factual delegada a baseline_gaps; el primario verifica antes de resolver.

## Resolution comments

2026-10-25, America/Mexico_City. Investigación factual revisada por el primario.
Se contrastaron directamente el anuncio oficial 6.1, el artículo del 20 de
abril y la guía de logs. Hay documentación positiva de archivo consultable
de errores y logs, y almacenamiento de spans en Parquet. La evidencia de
GoodSentry limitada a spans/transacciones no cubre automáticamente los otros
tipos. Contexto Java está anunciado; las fuentes consultadas no fijan cliente,
artefacto ni asociación reproducibles. Esta incertidumbre queda explícita en
la decisión de compatibilidad y no autoriza sacrificar Java.

[Informe y límites de evidencia](research/java-and-cold-archive.md).
La investigación no aprueba alcance ni acredita aceptación del producto.
