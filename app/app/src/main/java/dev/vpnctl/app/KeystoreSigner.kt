package dev.vpnctl.app

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import mobile.Signer

/**
 * The device's identity: a P-256 key that never leaves the Android Keystore.
 *
 * Go gets two things only — the public key as PKIX DER and a DER signature over
 * the SHA-256 of the message. Everything else stays inside the TEE. The
 * contract is pinned on the Go side by mobile.TestSignatureMatchesTheAgent and
 * gitsync.TestAuthMethodSignsLikeSSH, so a mismatch fails there and not on a
 * host at night.
 */
class KeystoreSigner(private val alias: String = DEFAULT_ALIAS) : Signer {

    override fun publicKeyDER(): ByteArray = publicKey().encoded

    override fun publicKeyPEM(): String {
        val der = publicKey().encoded
        val body = android.util.Base64.encodeToString(der, android.util.Base64.NO_WRAP)
        // 64 characters per line is what PEM readers expect
        val wrapped = body.chunked(64).joinToString("\n")
        return "-----BEGIN PUBLIC KEY-----\n$wrapped\n-----END PUBLIC KEY-----\n"
    }

    override fun sign(digest: ByteArray): ByteArray {
        // Go hashes and hands over the digest; "NONEwithECDSA" is the primitive
        // that signs a digest as-is. "SHA256withECDSA" would hash it again.
        val signature = Signature.getInstance("NONEwithECDSA")
        signature.initSign(privateKey())
        signature.update(digest)
        return signature.sign()
    }

    private fun keyStore(): KeyStore =
        KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }

    private fun publicKey() = keyStore().getCertificate(alias).publicKey

    private fun privateKey() = keyStore().getKey(alias, null) as java.security.PrivateKey

    /** Creates the key on first launch; a second call is a no-op. */
    fun ensure(): KeystoreSigner {
        val store = keyStore()
        if (store.containsAlias(alias)) return this

        val gen = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, ANDROID_KEYSTORE)
        gen.initialize(
            KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_SIGN or KeyProperties.PURPOSE_VERIFY)
                .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
                .setDigests(KeyProperties.DIGEST_NONE)
                .setUserAuthenticationRequired(false)
                .build()
        )
        gen.generateKeyPair()
        return this
    }

    companion object {
        private const val ANDROID_KEYSTORE = "AndroidKeyStore"
        const val DEFAULT_ALIAS = "vpnctl-identity"
    }
}
