# Evidencia reutilizable de GoodSentry

Investigación para «Identificar la evidencia reutilizable de GoodSentry».
Fecha local: 25 de octubre de 2026, America/Mexico_City.
Observación inicial: 2026-10-26T03:23:14Z.
Repositorio observado: /home/dev/projects/SentryGo.
HEAD y origin/main: `80e7298da5eb4ac87b1a1bb09a3949fc57c12c97`.
Otra sesión implementa activamente: este informe es una fotografía, no un estado vivo.
No se ejecutaron builds, pruebas, cambios de configuración ni publicación.

## Resultado

Existe evidencia suficiente para retomar por capacidades y entregables sin
reconstruir todas las sesiones. La auditoría global sigue abierta y no permite
declarar paridad completa o un porcentaje de avance.
La falta de un contrato finito aprobado continúa siendo una decisión pendiente;
este informe no decide qué sacrificar ni qué versión de GlitchTip adoptar.

## Qué autoridad tiene cada fuente

- `SPEC.md:5-15`: objetivo y aceptación observable; clean-room por documentación,
  contratos públicos y clientes black-box. GlitchTip es mínimo, no solo ingestión.
- `SPEC.md:17-31`: nueve áreas estables; la autoridad de diseño sigue en SPEC.
- `tasks/plan.md`: orden de incrementos y cierres; `tasks/todo.md:3` liga evidencia
  a los contratos. Un checkbox aislado no prueba cumplimiento.
- `docs/compatibility.md:90-112`: límites y brechas que deben reconciliarse con SPEC.
- `docs/development.md:11-25`: comandos y publicación existentes; no inventar otra CI.

## Cambio desde la retrospectiva anterior

La fotografía anterior en `1969dd3` tenía seis commits locales sin publicar.
Ese dato quedó obsoleto. La observación actual encuentra `80e7298` tanto en
HEAD como en origin/main. El único archivo ajeno sin seguimiento observado es
`tests/compat/otlp_python_smoke.py`; el nuevo tracker Wayfinder tampoco estaba
versionado al momento de la lectura. No se tocaron esos archivos.

El demo nativo ya cerró su aceptación específica. `tasks/todo.md:199-205` y
`tasks/plan.md:515-521` registran SDK oficial 0.17.4, crash real Crashpad,
símbolos del mismo ELF, función `goodsentry_demo_crash`, archivo `main.c`,
línea positiva y descarte del dump. Dos defectos de identidad ELF/Mach-O/BpEL
descubiertos durante esa prueba se corrigieron en `a0cd17e`.
Esto cierra Linux/amd64 con ese cliente, no todo el requisito nativo padre.

## Evidencia actual por etapa

| Capacidad | Evidencia observada | Límite |
|---|---|---|
| Arranque abierto y required | Auditoría `1969dd3`; README, SPEC y pruebas existentes | No se repitió runtime en esta investigación |
| Demo real Sentry Native | [Workflow 40263504129](https://github.com/jerolan/good-sentry/actions/runs/40263504129) success en `80e7298`; pasos de instalar/configurar/compilar/crash comprobados por API | No significa todas las arquitecturas/formatos |
| Símbolos ELF/Mach-O y BpEL | `a0cd17e`, registro `tasks/todo.md:204-205` y [CI 40256935013](https://github.com/jerolan/good-sentry/actions/runs/40256935013) | Reutilizar en su alcance exacto |
| Social GitHub/GitLab | `ee581a3`, proveedores TLS sintéticos y [CI 40260541363](https://github.com/jerolan/good-sentry/actions/runs/40260541363) | Sin recibo de cuenta real externa |
| Social Gitea | `80e7298`; registro de pruebas sintéticas `tasks/todo.md:630-634` | CI del HEAD seguía en curso al consultar; no hay recibo real externo |
| Imagen publicada más reciente observada | [Imagen 40261973982](https://github.com/jerolan/good-sentry/actions/runs/40261973982) success en `bab5a52` | Anterior al HEAD Gitea |
| Alertas | Payloads/HTTP local/PostgreSQL/browser documentados en `docs/compatibility.md:110` | Comprobantes externos abiertos |
| SDKs Go/Python/Node/browser, OTLP y recuperación | Matriz y evidencia declaradas en `docs/compatibility.md:112-130` | Referencias reutilizables; no auditoría independiente de cada log |

La [CI 40263504196](https://github.com/jerolan/good-sentry/actions/runs/40263504196)
del HEAD `80e7298` estaba `in_progress` en la lectura final de Actions de esta
investigación. No convertir ese estado transitorio en aprobación o bloqueo.

## Pendientes que siguen siendo preguntas de aceptación

Las casillas abiertas observadas están en `tasks/todo.md:173,540,626,630,678,682,1097,1124`.
Se solapan: nativo general, recibos de alertas repetidos por distintos niveles,
OAuth/OIDC externo, gates de Gitea y auditoría global. No medir progreso con ese conteo.

Además, `docs/compatibility.md:94-102` conserva integraciones automáticas con
repositorios, políticas adicionales de issues, WebAuthn, require2FA organizacional
y throttling organizacional no soportado. La auditoría debe decidir cómo se
relacionan con el mínimo de GlitchTip y los requisitos ya aprobados.
Eso no autoriza implementarlos ni excluirlos automáticamente.

Precisión para la comparación: `docs/compatibility.md:100` y
`docs/development.md` describen archivo frío de spans/transacciones. Eso no
demuestra archivo frío de logs; una coincidencia de nombre "archive" no prueba
equivalencia funcional. La matriz debe distinguir los tipos de datos.

## Guardrails y límites de entorno

La CI ya corre gofmt, vet, PostgreSQL integration/race y Docker build
(`.github/workflows/ci.yml:49-61`).
El workflow nativo construye binarios y ejecuta el crash real con variables
explícitas (`.github/workflows/native-demo.yml:57-81`).
La prueba general puede omitir el demo sin esos binarios; consultar el workflow
dedicado evita confundir compilación/skip con aceptación real.

Los errores históricos de GitHub/Docker son hechos de sus contextos de ejecución,
no bloqueos permanentes. `gh run list` y `gh run view` funcionaron en esta lectura.
No se cambió autenticación ni se evaluaron destinos externos.

## Información mínima para una sesión nueva

Propuesta de información de continuidad, todavía no contrato aprobado:

1. Identidad del contrato de cierre vigente y referencia congelada de GlitchTip.
2. HEAD observado, cambios locales del usuario y revisión/imagen publicada.
3. Próximo criterio abierto con su resultado observable y dependencia.
4. Evidencia aceptada por criterio: commit, cliente/plataforma, comando o workflow,
   resultado, artefacto y condición que invalidaría esa evidencia.
5. Bloqueo vigente: capacidad ausente en qué contexto, prueba de esa ausencia,
   acción necesaria y responsable. Tratar evidencia histórica como tal.

Basta cargar ese resumen y luego las secciones dueñas del criterio elegido.
El inventario completo de sesiones no es un prerrequisito demostrado.

## Auditoría adicional indispensable

- Acordar referencia y matriz mínima funcional de GlitchTip con el usuario.
- Reconciliar cada criterio de esa matriz con los requisitos aprobados y la
  evidencia localizada; marcar evidencia ausente y diferencias funcionales.
- Separar capacidades adicionales de Sentry antes de decidir sacrificios.
- Definir qué clientes, versiones, plataformas y comprobantes externos constituyen
  cierre, y qué cambios obligan a repetir qué pruebas.
- Revalidar estados transitorios de CI/imagen cuando se use este informe.

No se inspeccionó el historial completo ni todo el código. Los registros de
aceptación locales se usaron como evidencia documentada, no como reproducción
independiente. La verificación directa de Actions cubrió estado/commit y los
pasos del demo nativo; no se descargaron todos los logs o artefactos.
