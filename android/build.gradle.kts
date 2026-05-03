// Top-level build script.
//
// Versions des plugins centralisées dans gradle/libs.versions.toml (Version Catalog).
// `apply false` : déclare les plugins pour les sous-modules sans les appliquer ici.
plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.kotlin.android) apply false
}

// ==============================================================================
// Story 10.4 — Audit télémétrie zéro-tracking (FR-AND-8 / NFR-AND-8 / ADR-15).
//
// Inspecte le graphe complet de dépendances de chaque sous-module et fait
// échouer le build si l'un des modules canoniquement interdits est présent.
// La liste vit dans une seule constante (FORBIDDEN_TELEMETRY_GROUPS ci-dessous)
// qui sert de source de vérité unique pour la task ET pour AuditCITest.kt
// (anti-regression — cf. AC #5).
//
// Détection :
//   - Itère sur les configurations release{Runtime,Compile}Classpath ET
//     debug{Runtime,Compile}Classpath. Auditer debug aussi est important :
//     un dev qui ajouterait Crashlytics « juste en debug » verrait son
//     commit échouer en CI, ce qui force le respect d'ADR-15 même avant
//     le release.
//   - Match par préfixe groupId (`startsWith`). Inclusif sans risque de
//     false-positive — aucun groupId Android légitime ne commence par ces
//     préfixes.
//   - Récursif via `lenientConfiguration.allModuleDependencies` — détecte
//     les transitives. Un dev ne peut pas se cacher derrière une transitive.
//
// Échec :
//   - GradleException → exit code non-zero, build cassé.
//   - Message multi-ligne explicite avec liste des violations + références
//     ADR-15 / NFR-AND-8 / FR-AND-8.
//
// Coordination Story 12.2 : le pipeline release-android.yml ré-invoquera
// cette même task — pas de duplication de la liste canonique.
// ==============================================================================

val forbiddenTelemetryGroups = listOf(
    "com.google.firebase",
    "com.crashlytics.sdk.android",
    "io.sentry",
    "com.bugsnag",
    "com.mixpanel.android",
    "com.adjust.sdk",
    "io.branch.sdk.android",
    "com.amplitude",
)

val auditedConfigurations = listOf(
    "releaseRuntimeClasspath",
    "releaseCompileClasspath",
    "debugRuntimeClasspath",
    "debugCompileClasspath",
)

subprojects {
    tasks.register("auditTelemetryDependencies") {
        group = "verification"
        description =
            "Bloque le build si une dépendance télémétrie/analytics est présente (cohérent ADR-15)."
        doLast {
            val violations = mutableListOf<String>()
            val unresolvedReports = mutableListOf<String>()
            auditedConfigurations.forEach { configName ->
                val config = configurations.findByName(configName) ?: return@forEach
                val lenient = config.resolvedConfiguration.lenientConfiguration
                // Garde-fou : si une dépendance n'a pas pu être résolue (Maven
                // Central injoignable, version retirée, etc.), `lenient` la
                // skippe silencieusement. On collecte ces cas pour fail-loud
                // — un audit "qui passe" sans avoir tout vu serait trompeur.
                lenient.unresolvedModuleDependencies.forEach { unresolved ->
                    unresolvedReports +=
                        "[$configName] ${unresolved.selector.toString()} (résolution échouée)"
                }
                lenient.allModuleDependencies.forEach { dep ->
                    val moduleId = "${dep.moduleGroup}:${dep.moduleName}"
                    val matchedPrefix = forbiddenTelemetryGroups.firstOrNull { forbiddenPrefix ->
                        dep.moduleGroup.startsWith(forbiddenPrefix)
                    }
                    if (matchedPrefix != null) {
                        violations += "[$configName] $moduleId (cohérent ADR-15)"
                    }
                }
            }
            if (violations.isNotEmpty()) {
                val report = violations.distinct().joinToString("\n  - ", prefix = "  - ")
                throw GradleException(
                    """
                    |❌ Audit télémétrie échoué — dépendances interdites détectées :
                    |$report
                    |
                    |Cohérent ADR-15 (architecture.md) + NFR-AND-8 (prd.md l. 704) + FR-AND-8 (prd.md l. 616).
                    |Le client Android Le Voile n'embarque AUCUNE télémétrie, AUCUN crash reporter, AUCUN analytics.
                    |Retirez la dépendance offensive ou justifiez-la dans un ADR avant ré-introduction.
                    """.trimMargin()
                )
            }
            if (unresolvedReports.isNotEmpty()) {
                val report = unresolvedReports.distinct().joinToString("\n  - ", prefix = "  - ")
                throw GradleException(
                    """
                    |❌ Audit télémétrie incomplet — dépendances non résolues :
                    |$report
                    |
                    |L'audit ne peut pas garantir l'absence de modules interdits tant
                    |que toutes les dépendances sont résolues. Vérifiez la connectivité
                    |réseau (Maven Central) et relancez. Pour un audit local hors-ligne,
                    |peuplez le cache Gradle d'abord via `./gradlew --refresh-dependencies`.
                    """.trimMargin()
                )
            }
            logger.lifecycle("✓ Audit télémétrie passé pour ${project.path} — aucun module interdit détecté.")
        }
    }

    // Hook sur la lifecycle task `check` une fois que les plugins Android ont
    // été appliqués (sinon `check` n'existe pas encore). afterEvaluate garantit
    // que la task est en place avant qu'on cherche à dependsOn.
    afterEvaluate {
        tasks.findByName("check")?.dependsOn("auditTelemetryDependencies")
    }
}

// Task de convenance : `./gradlew auditAllTelemetryDependencies` agrège les
// audits des 2 modules en une seule invocation, pratique en local et symétrique
// avec l'invocation CI (qui appelle les 2 modules explicitement).
tasks.register("auditAllTelemetryDependencies") {
    group = "verification"
    description = "Audit télémétrie agrégé sur tous les modules Android."
    dependsOn(":app:auditTelemetryDependencies", ":levoile-core:auditTelemetryDependencies")
}
