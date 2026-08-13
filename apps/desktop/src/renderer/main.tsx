import "@vitejs/plugin-react/preamble";
import { createRoot } from "react-dom/client";

import { App } from "./app";
import "./styles.css";

const root = document.getElementById("root");

if (!root) {
  throw new Error("Renderer root element is missing");
}

createRoot(root).render(<App />);
