import { ArrowRight } from "lucide-react";
import { api } from "./api";
import { AppLink } from "./shell";
import { useRemote } from "./use-remote";

export function IntelligenceScreen({ navigate }: { navigate: (path: string) => void }) {
  const insights = useRemote("insights", api.insights);
  const paths = { documents: "/documents", banking: "/banking", bas: "/gst-bas" } as const;
  return (
    <div className="screen">
      <div className="page-heading">
        <div>
          <h1>Intelligence</h1>
          <p>Attention drawn from the current records in your books</p>
        </div>
      </div>
      <p className="supporting-copy">
        These are rule-based observations from your local data, not predictions or accounting
        advice.
      </p>
      {insights.state.status === "error" ? (
        <p className="notice error" role="alert">
          {insights.state.message}{" "}
          <button className="text-button" onClick={insights.reload} type="button">
            Retry
          </button>
        </p>
      ) : insights.state.status === "loading" ? (
        <p className="loading" role="status">
          Reading your books…
        </p>
      ) : (
        <section className="panel">
          {insights.state.value.insights.length ? (
            <ul className="insight-list">
              {insights.state.value.insights.map((insight) => (
                <li key={insight.id}>
                  <h2>{insight.title}</h2>
                  <p>{insight.detail}</p>
                  <AppLink className="text-link" navigate={navigate} to={paths[insight.kind]}>
                    View source <ArrowRight aria-hidden="true" />
                  </AppLink>
                </li>
              ))}
            </ul>
          ) : (
            <div className="empty-content">
              <h3>Nothing needs attention right now</h3>
              <p>Suggestions appear when your records need review</p>
            </div>
          )}
        </section>
      )}
    </div>
  );
}

export function ConnectionsScreen() {
  const capabilities = useRemote("capabilities", api.capabilities);
  return (
    <div className="screen">
      <div className="page-heading">
        <div>
          <h1>Connections</h1>
          <p>Local ways to work with your accounting data</p>
        </div>
      </div>
      {capabilities.state.status === "error" ? (
        <p className="notice error" role="alert">
          {capabilities.state.message}{" "}
          <button className="text-button" onClick={capabilities.reload} type="button">
            Retry
          </button>
        </p>
      ) : capabilities.state.status === "loading" ? (
        <p className="loading" role="status">
          Checking connections…
        </p>
      ) : (
        <div className="connection-list">
          <section className="panel">
            <div className="panel-heading">
              <div>
                <h2>Local API</h2>
                <p>{capabilities.state.value.api.description}</p>
              </div>
              <span className="status ready">
                {capabilities.state.value.api.available ? "Available locally" : "Unavailable"}
              </span>
            </div>
            <p className="supporting-copy">
              The application uses the local API for its accounting workflows.
            </p>
          </section>
          <section className="panel">
            <div className="panel-heading">
              <div>
                <h2>Model Context Protocol</h2>
                <p>{capabilities.state.value.mcp.description}</p>
              </div>
              <span
                className={`status ${capabilities.state.value.mcp.available ? "ready" : "pending"}`}
              >
                {capabilities.state.value.mcp.available ? "Enabled" : "Not enabled"}
              </span>
            </div>
            <p className="supporting-copy">
              Read-only tools · {capabilities.state.value.mcp.transport} transport. An explicit
              local token and an unlocked workspace are required.
            </p>
          </section>
          <section className="panel">
            <div className="panel-heading">
              <div>
                <h2>Intelligence</h2>
                <p>{capabilities.state.value.intelligence.description}</p>
              </div>
              <span className="status ready">
                {capabilities.state.value.intelligence.available ? "Available" : "Unavailable"}
              </span>
            </div>
          </section>
        </div>
      )}
    </div>
  );
}
