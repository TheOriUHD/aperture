import { Component, type ReactNode } from "react";

// A minimal render error boundary: it catches a throw in its subtree and renders a
// fallback instead of letting the error unmount the whole React tree. Extensions are
// third-party by design, so each row is wrapped in one — a single malformed
// extension shows an inline error, not a blank page.
export class ErrorBoundary extends Component<
  { fallback: (err: Error, reset: () => void) => ReactNode; children: ReactNode },
  { err: Error | null }
> {
  state: { err: Error | null } = { err: null };

  static getDerivedStateFromError(err: Error) {
    return { err };
  }

  reset = () => this.setState({ err: null });

  render() {
    if (this.state.err) return this.props.fallback(this.state.err, this.reset);
    return this.props.children;
  }
}
