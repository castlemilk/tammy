import { createRoot } from "react-dom/client";
import { App } from "./app";
import "@fontsource-variable/ibm-plex-sans/wght.css";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("Missing app root");
createRoot(root).render(<App />);
