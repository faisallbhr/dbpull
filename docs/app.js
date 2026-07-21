(function () {
  const defaultCommand = "curl -fsSL https://faisallbhr.github.io/dbpull/install.sh | sh";
  const commands = {
    linux: defaultCommand,
    macos: defaultCommand,
    windows: "go install github.com/faisallbhr/dbpull@latest",
    unsupported: defaultCommand,
  };

  const commandEl = document.querySelector("[data-install-command]");
  const copyButton = document.querySelector("[data-copy-command]");
  const statusEl = document.querySelector("[data-copy-status]");

  function detectPlatform() {
    const platform = `${navigator.userAgentData?.platform || navigator.platform || ""}`.toLowerCase();
    const userAgent = navigator.userAgent.toLowerCase();
    const value = `${platform} ${userAgent}`;

    if (value.includes("win")) return "windows";
    if (value.includes("mac")) return "macos";
    if (value.includes("linux") || value.includes("x11")) return "linux";
    return "unsupported";
  }

  function setStatus(message) {
    statusEl.textContent = message;
    if (message) {
      window.setTimeout(() => {
        statusEl.textContent = "";
      }, 2200);
    }
  }

  async function copyCommand() {
    const command = commandEl.textContent.trim();
    try {
      await navigator.clipboard.writeText(command);
      copyButton.textContent = "Copied";
      setStatus("Command copied.");
    } catch {
      const selection = window.getSelection();
      const range = document.createRange();
      range.selectNodeContents(commandEl);
      selection.removeAllRanges();
      selection.addRange(range);
      setStatus("Command selected. Press Ctrl+C to copy.");
    } finally {
      window.setTimeout(() => {
        copyButton.textContent = "Copy";
      }, 1400);
    }
  }

  const platform = detectPlatform();
  commandEl.textContent = commands[platform];
  copyButton.addEventListener("click", copyCommand);
})();
