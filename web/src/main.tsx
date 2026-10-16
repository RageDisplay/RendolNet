import { createRoot } from "react-dom/client";
import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/layout.css";
import "./styles/components.css";
import { App } from "./App";

try {
  const th = localStorage.getItem("rn.theme");
  if (th === "light" || th === "dark") document.documentElement.dataset.theme = th;
} catch {
  /* по умолчанию тёмная */
}

createRoot(document.getElementById("root")!).render(<App />);
