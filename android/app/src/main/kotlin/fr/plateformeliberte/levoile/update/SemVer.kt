package fr.plateformeliberte.levoile.update

/**
 * SemVer 2.0.0 minimal — `major.minor.patch[-pre][+build]`.
 * https://semver.org/spec/v2.0.0.html
 *
 * Story 12.5 — pas de dep externe (kotlinx-serialization out of scope).
 * Parse strict ; build metadata (`+build`) ignorée pour la comparaison
 * (cohérent SemVer §10).
 */
internal data class SemVer(
    val major: Int,
    val minor: Int,
    val patch: Int,
    val preRelease: String? = null,    // ex. "beta.1", "rc.2"
    val buildMetadata: String? = null, // ex. "20260503.abc123"
) : Comparable<SemVer> {

    override fun compareTo(other: SemVer): Int {
        val byMajor = major.compareTo(other.major)
        if (byMajor != 0) return byMajor
        val byMinor = minor.compareTo(other.minor)
        if (byMinor != 0) return byMinor
        val byPatch = patch.compareTo(other.patch)
        if (byPatch != 0) return byPatch
        // SemVer §11 : version sans pre-release > version avec pre-release.
        return when {
            preRelease == null && other.preRelease == null -> 0
            preRelease == null -> 1
            other.preRelease == null -> -1
            else -> comparePreRelease(preRelease, other.preRelease)
        }
    }

    private fun comparePreRelease(a: String, b: String): Int {
        val partsA = a.split(".")
        val partsB = b.split(".")
        val maxLen = maxOf(partsA.size, partsB.size)
        for (i in 0 until maxLen) {
            val pa = partsA.getOrNull(i)
            val pb = partsB.getOrNull(i)
            if (pa == null) return -1
            if (pb == null) return 1
            val intA = pa.toIntOrNull()
            val intB = pb.toIntOrNull()
            val cmp = when {
                intA != null && intB != null -> intA.compareTo(intB)
                intA != null -> -1   // SemVer §11.4.3 : numeric < alphanumeric
                intB != null -> 1
                else -> pa.compareTo(pb)
            }
            if (cmp != 0) return cmp
        }
        return 0
    }

    companion object {
        // Optional `v` prefix toleré (cohérent GitHub release tags `v1.2.3`).
        private val REGEX = Regex(
            "^v?(\\d+)\\.(\\d+)\\.(\\d+)(?:-([0-9A-Za-z.-]+))?(?:\\+([0-9A-Za-z.-]+))?$",
        )

        fun parse(s: String): SemVer? {
            val cleaned = s.trim()
            val m = REGEX.matchEntire(cleaned) ?: return null
            return SemVer(
                major = m.groupValues[1].toInt(),
                minor = m.groupValues[2].toInt(),
                patch = m.groupValues[3].toInt(),
                preRelease = m.groupValues[4].takeIf { it.isNotEmpty() },
                buildMetadata = m.groupValues[5].takeIf { it.isNotEmpty() },
            )
        }
    }
}
