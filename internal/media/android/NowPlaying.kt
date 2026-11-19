package dev.mygo

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.media.MediaMetadata
import android.media.session.MediaSession
import android.media.session.PlaybackState
import android.os.IBinder
import android.util.Base64
import org.json.JSONObject

private const val PLUGIN = "pudmusic_media"
private const val CHANNEL = "pudmusic_playback"
private const val NOTIFICATION = 4101

class PudMusicMediaPlugin : MyGoPlugin {
    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        if (method != "publish") {
            reply.failure(PluginFailure("NoMethod", method))
            return
        }
        try {
            PudMusicPlayback.publish(PluginHost.context, args)
            reply.success(null)
        } catch (e: RuntimeException) {
            reply.failure(PluginFailure("MediaSession", e.message ?: e.javaClass.simpleName))
        }
    }
}

class PudMusicPlaybackService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        PudMusicPlayback.serviceStarting = false
        val notification = PudMusicPlayback.notification(this)
        if (notification == null) {
            startForeground(NOTIFICATION, PudMusicPlayback.notificationBuilder(this).build())
            stopSelfResult(startId)
            return START_NOT_STICKY
        }
        startForeground(NOTIFICATION, notification)
        PudMusicPlayback.foreground = this
        intent?.action?.let { PudMusicPlayback.command(it) }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        if (PudMusicPlayback.foreground === this) PudMusicPlayback.foreground = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        super.onDestroy()
    }
}

private object PudMusicPlayback {
    var foreground: PudMusicPlaybackService? = null
    var serviceStarting = false
    private var session: MediaSession? = null
    private var state = "none"
    private var title = ""
    private var artist = ""
    private var duration = 0L
    private var artworkURL = ""
    private var artwork: Bitmap? = null

    fun publish(context: Context, args: JSONObject) {
        val previousState = state
        state = args.optString("state", "none")
        if (state == "none") {
            session?.release()
            session = null
            artworkURL = ""
            artwork = null
            stopService(context)
            return
        }

        val newTitle = args.optString("title")
        val newArtist = args.optString("artist")
        val newDuration = (args.optDouble("duration", 0.0) * 1000).toLong()
        val newURL = args.optString("artworkURL")
        val newArtwork = args.optString("artwork")
        val metadataChanged = session == null || newTitle != title || newArtist != artist ||
            newDuration != duration || newURL != artworkURL || newArtwork.isNotEmpty()
        title = newTitle
        artist = newArtist
        duration = newDuration
        if (newURL != artworkURL) artwork = null
        artworkURL = newURL
        if (newArtwork.isNotEmpty()) artwork = decodeArtwork(newArtwork)

        val media = session ?: createSession(context).also { session = it }
        if (metadataChanged) {
            val metadata = MediaMetadata.Builder()
                .putString(MediaMetadata.METADATA_KEY_TITLE, title)
                .putString(MediaMetadata.METADATA_KEY_ARTIST, artist)
                .putLong(MediaMetadata.METADATA_KEY_DURATION, duration)
                .putBitmap(MediaMetadata.METADATA_KEY_ART, artwork)
            media.setMetadata(metadata.build())
        }
        val playbackState = when (state) {
            "playing" -> PlaybackState.STATE_PLAYING
            "stopped" -> PlaybackState.STATE_STOPPED
            else -> PlaybackState.STATE_PAUSED
        }
        var actions = PlaybackState.ACTION_PLAY or PlaybackState.ACTION_PAUSE or
            PlaybackState.ACTION_PLAY_PAUSE or PlaybackState.ACTION_SKIP_TO_NEXT or
            PlaybackState.ACTION_SKIP_TO_PREVIOUS or PlaybackState.ACTION_STOP
        if (duration > 0) actions = actions or PlaybackState.ACTION_SEEK_TO
        media.setPlaybackState(PlaybackState.Builder().setActions(actions).setState(
            playbackState, (args.optDouble("position", 0.0) * 1000).toLong(),
            if (state == "playing") 1f else 0f).build())
        media.isActive = state != "stopped"

        if (state == "stopped") {
            stopService(context)
        } else if (state == "playing" && foreground == null && !serviceStarting) {
            context.startForegroundService(Intent(context, PudMusicPlaybackService::class.java))
            serviceStarting = true
        } else if (foreground != null && (metadataChanged || previousState != state)) {
            notification(context)?.let { manager(context).notify(NOTIFICATION, it) }
        }
    }

    private fun stopService(context: Context) {
        foreground?.let { service ->
            foreground = null
            service.stopForeground(Service.STOP_FOREGROUND_REMOVE)
            context.stopService(Intent(context, PudMusicPlaybackService::class.java))
        }
    }

    private fun createSession(context: Context): MediaSession = MediaSession(context, "PudMusic").apply {
        setSessionActivity(openApp(context))
        setCallback(object : MediaSession.Callback() {
            override fun onPlay() = command("play")
            override fun onPause() = command("pause")
            override fun onSkipToNext() = command("next")
            override fun onSkipToPrevious() = command("prev")
            override fun onStop() = command("stop")
            override fun onSeekTo(pos: Long) = command("seek", pos)
        })
    }

    fun command(name: String, pos: Long = 0) {
        if (state == "none") return
        PluginHost.emit(PLUGIN, "command", JSONObject().put("command", name).put("position", pos / 1000.0))
    }

    fun notification(context: Context): Notification? {
        val media = session ?: return null
        if (state == "stopped") return null
        val playing = state == "playing"
        return notificationBuilder(context)
            .setContentTitle(title)
            .setContentText(artist)
            .setContentIntent(openApp(context))
            .setLargeIcon(artwork)
            .setOnlyAlertOnce(true)
            .setVisibility(Notification.VISIBILITY_PUBLIC)
            .addAction(action(context, android.R.drawable.ic_media_previous, "上一首", "prev"))
            .addAction(if (playing) action(context, android.R.drawable.ic_media_pause, "暂停", "pause")
                else action(context, android.R.drawable.ic_media_play, "播放", "play"))
            .addAction(action(context, android.R.drawable.ic_media_next, "下一首", "next"))
            .setDeleteIntent(control(context, "stop"))
            .setStyle(Notification.MediaStyle().setMediaSession(media.sessionToken).setShowActionsInCompactView(0, 1, 2))
            .build()
    }

    fun notificationBuilder(context: Context): Notification.Builder {
        manager(context).createNotificationChannel(NotificationChannel(CHANNEL, "音乐播放", NotificationManager.IMPORTANCE_LOW))
        return Notification.Builder(context, CHANNEL).setSmallIcon(android.R.drawable.ic_media_play)
    }

    private fun manager(context: Context) = context.getSystemService(NotificationManager::class.java)

    private fun openApp(context: Context): PendingIntent? = context.packageManager.getLaunchIntentForPackage(context.packageName)?.let {
        PendingIntent.getActivity(context, 0, it, PendingIntent.FLAG_IMMUTABLE)
    }

    private fun control(context: Context, command: String): PendingIntent = PendingIntent.getService(context, 0,
        Intent(context, PudMusicPlaybackService::class.java).setAction(command), PendingIntent.FLAG_IMMUTABLE)

    private fun action(context: Context, icon: Int, label: String, command: String): Notification.Action =
        Notification.Action.Builder(android.graphics.drawable.Icon.createWithResource(context, icon), label, control(context, command)).build()

    private fun decodeArtwork(encoded: String): Bitmap? {
        val data = Base64.decode(encoded, Base64.DEFAULT)
        val options = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(data, 0, data.size, options)
        var sample = 1
        while (maxOf(options.outWidth, options.outHeight) / sample > 1024) sample *= 2
        options.inJustDecodeBounds = false
        options.inSampleSize = sample
        return BitmapFactory.decodeByteArray(data, 0, data.size, options)
    }
}
