import { Component, type ErrorInfo, type ReactNode } from 'react';

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('ErrorBoundary caught:', error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <div className="min-h-screen flex items-center justify-center bg-gray-950 text-gray-100">
          <div className="bg-gray-900 border border-red-800 rounded-xl p-8 max-w-lg w-full">
            <h1 className="text-xl font-bold text-red-400 mb-3">Something went wrong</h1>
            <pre className="text-xs text-gray-400 bg-gray-800 rounded p-3 overflow-auto max-h-48">
              {this.state.error.message}
            </pre>
            <button
              onClick={() => this.setState({ error: null })}
              className="mt-4 px-4 py-2 rounded bg-blue-700 hover:bg-blue-600 text-sm font-semibold"
            >
              Try again
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
