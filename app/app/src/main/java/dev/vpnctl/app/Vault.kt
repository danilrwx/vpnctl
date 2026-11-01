package dev.vpnctl.app

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.io.File
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * The age secret cannot live in the Keystore (X25519 is not something it
 * generates), so it is stored as ciphertext under an AES-GCM key that does live
 * there. The key never leaves the TEE; the bytes on disk are useless without it.
 *
 * This is weaker than the SSH key, which signs inside the Keystore — say it out
 * loud rather than pretend otherwise.
 */
class Vault(private val dir: File, private val alias: String = DEFAULT_ALIAS) {

    private fun keyStore(): KeyStore =
        KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }

    private fun secretKey(): SecretKey {
        val store = keyStore()
        (store.getEntry(alias, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }

        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        gen.init(
            KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setUserAuthenticationRequired(false)
                .build()
        )

        return gen.generateKey()
    }

    /** put seals text and writes it where the app keeps private files. */
    fun put(name: String, text: String) {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())

        val sealed = cipher.iv + cipher.doFinal(text.toByteArray())
        File(dir, name).writeBytes(sealed)
    }

    /** get returns the text, or null when nothing was stored. */
    fun get(name: String): String? {
        val file = File(dir, name)
        if (!file.exists()) return null

        val bytes = file.readBytes()
        if (bytes.size <= IV_SIZE) return null

        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, secretKey(), GCMParameterSpec(128, bytes.copyOfRange(0, IV_SIZE)))

        return String(cipher.doFinal(bytes.copyOfRange(IV_SIZE, bytes.size)))
    }

    companion object {
        private const val ANDROID_KEYSTORE = "AndroidKeyStore"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private const val IV_SIZE = 12
        const val DEFAULT_ALIAS = "vpnctl-vault"
    }
}
