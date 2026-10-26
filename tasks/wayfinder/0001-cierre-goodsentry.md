---
id: WF-0001
title: Cerrar GoodSentry con un contrato estable
status: closed
labels: [wayfinder:map]
parent: null
assignee: null
blocked_by: []
---

## Destination

Acordar un contrato finito de cierre de GoodSentry: mínimo funcional de una
referencia congelada de GlitchTip, evidencia exigible, sacrificios expresos y
una instrucción de Goal que permita llegar al mismo fin desde nuevas sesiones.

## Notes

Planificación. No implementar funcionalidades ni modificar el Goal activo.
Consultar curated:wayfinder, curated:writing-for-agents y, para decisiones
con el usuario, curated:grilling y curated:domain-modeling.

Acuerdos del usuario del 25 de octubre de 2026, America/Mexico_City:
- Equivalencia funcional, sin reproducir cada API o detalle interno de GlitchTip.
- Las integraciones anunciadas como verificadas necesitan comprobantes externos.
- Capacidades adicionales de Sentry pueden aplazarse por complejidad; cada
  sacrificio se decidirá expresamente. La lista general de exclusiones se decide por separado; conservar las excepciones expresas registradas.
- El cierre debe persistir entre sesiones y proveedores.

El estado actual sirve para identificar distancia y evidencia reutilizable;
no se presupone que reconstruir todo el historial sea un prerrequisito.
Verificar qué conocimiento hace falta para cada decisión.

Usar el [registro local](README.md). El mapa es un índice de decisiones;
SPEC.md, tasks/plan.md y tasks/todo.md siguen siendo los artefactos de producto.
Respetar main y las preferencias de trabajo directo del usuario: guardar
investigación en archivos disjuntos, sin ramas research/ ni PR.

## Decisions so far

- [Fijar la referencia pública de GlitchTip](0002-referencia-glitchtip.md): investigación de la referencia candidata y sus fuentes; su adopción se resolvió en la decisión de flujos.
- [Identificar la evidencia reutilizable de GoodSentry](0003-evidencia-goodsentry.md): existe una fotografía por commit y evidencia reutilizable para continuar sin reconstruir el historial completo.

- [Precisar contexto Java y archivo frío de la referencia](0008-contexto-java-archivo-frio.md): hay documentación de archivo de logs y errores, distinta de la evidencia limitada a spans/transacciones.

- [Evaluar utilidad y esfuerzo del contexto Java](0009-viabilidad-contexto-java.md): el enriquecimiento es conocido, pero no se demostró trivial ni fundamental; la decisión de flujos registra su aplazamiento aprobado.

- [Acordar los flujos que prueban equivalencia con GlitchTip](0004-contrato-compatibilidad.md): aprobados referencia, cobertura, clientes y criterios observables, con aplazamiento de fuentes Java y prueba nativa representativa.

- [Decidir qué capacidades de Sentry se aplazan](0005-sacrificios.md): aprobados tres aplazamientos expresos, conservando las capacidades y controles existentes.

- [Acordar las pruebas externas y sus dependencias](0006-evidencia-externa.md): webhook genérico con evidencia de recepción y reintentos; Grafana conserva su criterio y se aplazan comprobantes de proveedores y GitLab.

- [Aprobar el contrato de cierre y su Goal reanudable](0007-goal-reanudable.md): aprobados contrato y prompt; integración canónica y ejecución quedan para pasos posteriores.

## Not yet specified

No quedan decisiones abiertas necesarias para acordar el contrato de cierre.
El mapa de planificación termina con la aprobación registrada. La integración
canónica y la ejecución siguen pendientes; cualquier contradicción concreta
que aparezca se tratará conforme al contrato aprobado, sin inferir cambios
de alcance ni presentar el producto como terminado.

## Out of scope

Implementar el producto, ejecutar aceptación completa, cambiar proveedores,
credenciales o facturación, y reactivar o sustituir el Goal de otra sesión.
La paridad futura con novedades de Sentry/GlitchTip queda fuera de este mapa.
