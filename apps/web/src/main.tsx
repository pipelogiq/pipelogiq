import { createRoot } from "react-dom/client";

// Self-hosted so the dashboard renders identically behind a proxy or air-gapped.
// The variable builds carry every weight the design system uses.
import "@fontsource-variable/inter";
import "@fontsource-variable/jetbrains-mono";

import App from "./App.tsx";
import "./index.css";

createRoot(document.getElementById("root")!).render(<App />);
