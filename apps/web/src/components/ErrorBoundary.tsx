import { Component, type ErrorInfo, type ReactNode } from 'react';
import { Button } from '@/components/ui/button';

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

/**
 * Keeps a rendering error on one screen from blanking the whole dashboard.
 * The message is shown so an operator can report it; reloading recovers.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Unhandled rendering error', error, info.componentStack);
  }

  render() {
    if (!this.state.error) {
      return this.props.children;
    }
    return (
      <div className="flex min-h-screen items-center justify-center bg-background p-6">
        <div className="max-w-lg rounded-xl border border-border bg-card p-8 text-center">
          <h1 className="text-lg font-semibold text-foreground">Something went wrong on this screen</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            The rest of Pipelogiq is unaffected. Reload to continue; if it happens again, send the message below to support.
          </p>
          <pre className="mt-4 overflow-x-auto rounded-md bg-muted p-3 text-left text-xs text-muted-foreground">
            {this.state.error.message}
          </pre>
          <Button className="mt-6" onClick={() => window.location.reload()}>
            Reload
          </Button>
        </div>
      </div>
    );
  }
}
