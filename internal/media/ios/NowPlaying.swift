import AVFoundation
import ImageIO
import MediaPlayer
import UIKit

final class PudMusicMediaPlugin: MyGoPlugin {
    private var artworkURL = ""
    private var artwork: MPMediaItemArtwork?
    private var targets: [(MPRemoteCommand, Any)] = []
    private var categoryError: Error?

    init() {
        do {
            try AVAudioSession.sharedInstance().setCategory(.playback)
        } catch {
            categoryError = error
        }
    }

    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        guard method == "publish" else {
            reply(.failure(PluginFailure("NoMethod", method)))
            return
        }
        if let categoryError {
            reply(.failure(PluginFailure("AudioSession", categoryError.localizedDescription)))
            return
        }
        let center = MPNowPlayingInfoCenter.default()
        let state = args["state"] as? String ?? "none"
        if state == "none" {
            center.nowPlayingInfo = nil
            for (command, target) in targets {
                command.removeTarget(target)
                command.isEnabled = false
            }
            targets.removeAll()
            artwork = nil
            artworkURL = ""
            reply(.success(nil))
            return
        }

        if targets.isEmpty {
            let commands = MPRemoteCommandCenter.shared()
            register(commands.playCommand, "play")
            register(commands.pauseCommand, "pause")
            register(commands.togglePlayPauseCommand, "toggle")
            register(commands.nextTrackCommand, "next")
            register(commands.previousTrackCommand, "prev")
            register(commands.stopCommand, "stop")
            register(commands.changePlaybackPositionCommand, "seek")
        }

        let url = args["artworkURL"] as? String ?? ""
        if url != artworkURL {
            artwork = nil
            artworkURL = url
        }
        if let encoded = args["artwork"] as? String,
           let data = Data(base64Encoded: encoded),
           let source = CGImageSourceCreateWithData(data as CFData, nil),
           let thumbnail = CGImageSourceCreateThumbnailAtIndex(source, 0, [
               kCGImageSourceCreateThumbnailFromImageAlways: true,
               kCGImageSourceCreateThumbnailWithTransform: true,
               kCGImageSourceThumbnailMaxPixelSize: 1024,
           ] as CFDictionary) {
            let image = UIImage(cgImage: thumbnail)
            artwork = MPMediaItemArtwork(boundsSize: image.size) { _ in image }
        }

        let duration = args["duration"] as? Double ?? 0
        let position = args["position"] as? Double ?? 0
        MPRemoteCommandCenter.shared().changePlaybackPositionCommand.isEnabled = duration > 0
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: args["title"] as? String ?? "",
            MPNowPlayingInfoPropertyElapsedPlaybackTime: position,
            MPNowPlayingInfoPropertyPlaybackRate: state == "playing" ? (args["rate"] as? Double ?? 1.0) : 0.0,
        ]
        if let artist = args["artist"] as? String, !artist.isEmpty {
            info[MPMediaItemPropertyArtist] = artist
        }
        if duration > 0 { info[MPMediaItemPropertyPlaybackDuration] = duration }
        if let artwork { info[MPMediaItemPropertyArtwork] = artwork }
        center.nowPlayingInfo = info
        reply(.success(nil))
    }

    private func register(_ command: MPRemoteCommand, _ name: String) {
        command.isEnabled = true
        let target = command.addTarget { event in
            let position = (event as? MPChangePlaybackPositionCommandEvent)?.positionTime ?? 0
            PluginHost.emit("pudmusic_media", "command", ["command": name, "position": position])
            return .success
        }
        targets.append((command, target))
    }
}
