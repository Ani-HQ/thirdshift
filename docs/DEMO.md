# Demo Storyboard

Target length: 20 to 30 seconds.

1. Windows terminal opens on the install command:

   ```powershell
   irm https://github.com/Ani-HQ/thirdshift/releases/latest/download/install.ps1 | iex
   ```

2. `thirdshift doctor` shows Windows x64, NVIDIA GPU, VRAM, RAM, disk, HTTPS, and WSS checks.
3. `thirdshift login --invite ... --coordinator ...` registers the node and prints the node id.
4. `thirdshift start` connects the outbound session and reports the model ready.
5. Browser shows `http://127.0.0.1:8081/status` with connected nodes and available model count.
6. Developer terminal sends a `/v1/chat/completions` curl request for `thirdshift-tiny-chat-v1`.
7. Response shows an OpenAI-compatible completion with `thirdshift.job_id` and `thirdshift.attempts`.
8. `thirdshift card` prints jobs accepted, tokens served, and credit earned.

Keep the capture honest: show alpha labels and the non-sensitive workload boundary. Do not imply guaranteed earnings or confidential compute.
