package dev.mygo

import android.app.Activity
import android.content.ContentResolver
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.provider.DocumentsContract
import android.provider.OpenableColumns
import java.io.File
import java.io.IOException
import java.nio.file.Files
import java.util.Locale
import org.json.JSONArray
import org.json.JSONObject

class PudMusicDocumentsPlugin : MyGoPlugin {
    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        if (method == "openLoginConfirmation") {
            val activity = PluginHost.activity
            val uri = Uri.parse(args.optString("URL"))
            if (activity == null || uri.scheme != "bilibili" || uri.host != "browser") {
                reply.failure(PluginFailure("OpenLogin", "Bilibili confirmation is unavailable"))
                return
            }
            try {
                activity.startActivity(Intent(Intent.ACTION_VIEW, uri))
                reply.success(null)
            } catch (error: RuntimeException) {
                reply.failure(PluginFailure("OpenLogin", "Could not open Bilibili"))
            }
            return
        }
        if (method == "takeShares") {
            reply.success(PudMusicShareActivity.takeShares())
            return
        }
        if (method != "pickDirectory" && method != "pickFiles") {
            reply.failure(PluginFailure("NoMethod", method))
            return
        }
        val extensions = args.optJSONArray("Extensions") ?: JSONArray()
        val supported = (0 until extensions.length()).map { extensions.getString(it) }.toSet()
        val directory = method == "pickDirectory"
        val intent = if (directory) Intent(Intent.ACTION_OPEN_DOCUMENT_TREE) else Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
            addCategory(Intent.CATEGORY_OPENABLE)
            type = "audio/*"
            putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
        }
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        PluginHost.startForResult(intent) { result, data ->
            if (result != Activity.RESULT_OK || data == null) {
                reply.success(JSONObject())
            } else if (directory) {
                val tree = data.data
                if (tree == null) reply.success(JSONObject()) else Thread { importTree(tree, supported, reply) }.start()
            } else {
                val documents = data.clipData?.let { clip -> (0 until clip.itemCount).mapNotNull { clip.getItemAt(it).uri } }
                    ?: listOfNotNull(data.data)
                val context = PluginHost.context
                Thread { reply.success(stageSelection(context, documents, "pudmusic-pick-")) }.start()
            }
        }
    }

    private fun importTree(tree: Uri, supported: Set<String>, reply: PluginReply) {
        val context = PluginHost.context
        var staging: File? = null
        try {
            val directory = Files.createTempDirectory(context.cacheDir.toPath(), "pudmusic-saf-").toFile()
            staging = directory
            val files = JSONArray()
            val pending = ArrayDeque<String>()
            val visited = HashSet<String>()
            pending.add(DocumentsContract.getTreeDocumentId(tree))
            val resolver = context.contentResolver
            var failed = 0
            while (pending.isNotEmpty()) {
                val id = pending.removeFirst()
                if (!visited.add(id)) continue
                val children = try {
                    directoryEntries(resolver, tree, id)
                } catch (error: Exception) {
                    failed++
                    continue
                }
                for (child in children) {
                    if (child.directory) {
                        pending.add(child.id)
                    } else if (".${child.name.substringAfterLast('.', "").lowercase(Locale.ROOT)}" in supported) {
                        try {
                            val document = DocumentsContract.buildDocumentUriUsingTree(tree, child.id)
                            files.put(stageDocument(resolver, document, child.name, directory))
                        } catch (error: Exception) {
                            failed++
                        }
                    }
                }
            }
            reply.success(JSONObject().put("Directory", directory.absolutePath).put("Files", files).put("Failed", failed))
        } catch (error: Exception) {
            staging?.deleteRecursively()
            reply.failure(PluginFailure("ReadDirectory", error.javaClass.simpleName))
        }
    }

    private data class Entry(val id: String, val name: String, val directory: Boolean)

    private fun directoryEntries(resolver: ContentResolver, tree: Uri, id: String): List<Entry> {
        val children = DocumentsContract.buildChildDocumentsUriUsingTree(tree, id)
        val columns = arrayOf(
            DocumentsContract.Document.COLUMN_DOCUMENT_ID,
            DocumentsContract.Document.COLUMN_DISPLAY_NAME,
            DocumentsContract.Document.COLUMN_MIME_TYPE,
        )
        val cursor = resolver.query(children, columns, null, null, null) ?: throw IOException("Directory unavailable")
        return cursor.use {
            buildList {
                while (it.moveToNext()) {
                    add(Entry(it.getString(0), it.getString(1) ?: "", it.getString(2) == DocumentsContract.Document.MIME_TYPE_DIR))
                }
            }
        }
    }
}

private fun stageDocument(resolver: ContentResolver, uri: Uri, name: String, directory: File): String {
    require(uri.scheme == ContentResolver.SCHEME_CONTENT && name.isNotBlank() && File(name).name == name && name != "." && name != "..")
    val destination = Files.createTempDirectory(directory.toPath(), "document-").toFile()
    val output = File(destination, name)
    try {
        (resolver.openInputStream(uri) ?: throw IOException("Document unavailable")).use { input ->
            output.outputStream().use { input.copyTo(it) }
        }
        return output.absolutePath
    } catch (error: Exception) {
        destination.deleteRecursively()
        throw error
    }
}

private fun stageSelection(context: android.content.Context, documents: List<Uri>, prefix: String): JSONObject {
    val staging = runCatching { Files.createTempDirectory(context.cacheDir.toPath(), prefix).toFile() }.getOrNull()
    val files = JSONArray()
    var failed = 0
    for (uri in documents) {
        try {
            if (staging == null || uri.scheme != ContentResolver.SCHEME_CONTENT) throw IOException("Document unavailable")
            val resolver = context.contentResolver
            val name = resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use {
                if (it.moveToFirst()) it.getString(0) else null
            } ?: throw IOException("Document has no name")
            files.put(stageDocument(resolver, uri, name, staging))
        } catch (error: Exception) {
            failed++
        }
    }
    return JSONObject().put("Directory", staging?.absolutePath ?: "").put("Files", files).put("Failed", failed)
}

class PudMusicShareActivity : Activity() {
    companion object {
        private val pending = ArrayDeque<JSONObject>()

        fun takeShares(): JSONArray = synchronized(pending) {
            val result = JSONArray()
            while (pending.isNotEmpty()) result.put(pending.removeFirst())
            result
        }
    }

    @Suppress("DEPRECATION")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (savedInstanceState != null) { finish(); return }
        val uris = when (intent.action) {
            Intent.ACTION_SEND -> listOfNotNull(intent.getParcelableExtra<Uri>(Intent.EXTRA_STREAM))
            Intent.ACTION_SEND_MULTIPLE -> intent.getParcelableArrayListExtra<Uri>(Intent.EXTRA_STREAM).orEmpty()
            else -> emptyList()
        }
        if (uris.isEmpty()) { finish(); return }
        Thread {
            val result = stageSelection(this, uris, "pudmusic-share-")
            synchronized(pending) { pending.add(result) }
            runOnUiThread {
                packageManager.getLaunchIntentForPackage(packageName)?.let { startActivity(it.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP)) }
                runCatching { PluginHost.emit("pudmusic_documents", "shared") }
                finish()
            }
        }.start()
    }
}
