# First Minecraft server (M2)

## Prepare the host

Use a Linux host with sufficient free RAM/disk and an installed Java runtime.
Chunkd supports release versions from 1.20.5 onward in this batch. It reads the
release's Java requirement from Mojang metadata; the baseline is Java 21 for
1.20.5–1.21.11 and Java 25 for 26.1 onward. Paper builds must be stable.

Put `java` on PATH, set JAVA_HOME, or set CHUNKD_JAVA_PATHS to additional absolute
Java executable paths (colon separated on Linux, semicolon on Windows). The UI
shows successfully detected paths/versions. Chunkd never installs Java for you.

The daemon needs outbound HTTPS to Mojang/Paper metadata and artifact hosts. The
current [Paper download-service contract](https://docs.papermc.io/misc/downloads-service/)
uses Fill v3; Chunkd sends an identifying User-Agent and checks published hashes.
See [Paper's Java requirements](https://docs.papermc.io/paper/getting-started/)
and the [official Minecraft server page](https://www.minecraft.net/en-us/download/server).

## Create and run

1. Start Chunkd and sign in as the local administrator.
2. Choose **Create server**, enter a name, choose Vanilla or Paper, a release
   version and detected Java runtime, maximum memory and an unprivileged port.
   The default game port is 25565; Chunkd rejects ports assigned to another
   server record. Check external processes/firewall configuration separately.
3. Read the linked Minecraft EULA. Check acceptance only if you agree, then
   choose **Install server**. This queues an asynchronous job; it does not start
   Java or automatically accept terms on your behalf.
4. Follow recent jobs. Installation validates metadata, downloads/verifies a JAR
   into a private staging directory, writes eula.txt and server.properties, and
   promotes the directory to `data/servers/<ID>/`. A completed install is Stopped.
5. Choose **Start**. The Start job reports process launch; the server changes
   from Starting to Running when Minecraft emits its readiness message. Large
   first-world generation can take time. Failures appear in server/job messages.
6. Join from a matching Minecraft Java client using the host's IP/hostname and
   the configured port. Use `localhost:<port>` only when the game client runs on
   the same host. Chunkd does not open firewall ports or configure your router.
7. **Stop** sends `stop` to stdin and waits up to 30 seconds before terminating
   an unresponsive owned process. **Restart** waits for the old process to exit
   before launching a replacement.

Failed installations have a Retry installation control. The selected release
and runtime remain unchanged. Never overwrite an existing server directory to
repair an interrupted promotion: preserve its contents for operator inspection.

## Lifecycle and recovery limits

States are Installing, Stopped, Starting, Running, Stopping, Crashed, Failed and
Unknown. Jobs are Queued, Running, Succeeded or Failed. Conflicting operations
are refused rather than launching parallel Java processes. Logging/console,
resource metrics and editing server.properties through the UI come in M3.

Normal daemon shutdown attempts to stop every owned server. After daemon failure,
persisted active records become Unknown and cannot be started, stopped or
restarted through M2. Do not infer ownership from a PID alone. An operator must
stop Chunkd, establish whether the Java process still belongs to this server,
stop it safely if appropriate, back up the database, and only after verifying no
process remains reset that server's state to Stopped and PID to 0 offline. Full
reconciliation and recovery are C-17.

## Verification

Automated tests exercise checksum-verified fixture downloads, native-process
parallel-start protection, crash/forced-stop handling, job persistence and the
authenticated OData workflow. CI also compiles a small local Java fixture into
a JAR and verifies real JVM launch/readiness/graceful stop under the race detector.
It does not download/start Minecraft or consent to its EULA.

Before deploying this batch for players, perform the workflow above with an
actual Vanilla and Paper server on a disposable Linux host and join each with a
matching game client. That live game-client check is manual and is not claimed
by the automated fixture tests.
