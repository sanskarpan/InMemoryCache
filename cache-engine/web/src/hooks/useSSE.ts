import { useEffect, useRef, useState } from 'react';
import { getAuthenticatedSSEURL } from '../api/client';

export function useSSE<T>(url: string, onData?: (value: T) => void): T | null {
  const [data, setData] = useState<T | null>(null);
  const esRef = useRef<EventSource | null>(null);
  const onDataRef = useRef(onData);

  useEffect(() => {
    onDataRef.current = onData;
  }, [onData]);

  useEffect(() => {
    let retryTimeout: ReturnType<typeof setTimeout>;
    let cancelled = false;

    const connect = async () => {
      try {
        const resolvedURL = await getAuthenticatedSSEURL(url);
        if (cancelled) {
          return;
        }
        const es = new EventSource(resolvedURL);
        esRef.current = es;

        es.onmessage = (e) => {
          try {
            const next = JSON.parse(e.data) as T;
            setData(next);
            onDataRef.current?.(next);
          } catch (err) {
            console.warn('useSSE: failed to parse SSE message', err);
          }
        };

        es.onerror = () => {
          es.close();
          retryTimeout = setTimeout(() => {
            void connect();
          }, 2000);
        };
      } catch (err) {
        console.warn('useSSE: failed to establish SSE connection', err);
        retryTimeout = setTimeout(() => {
          void connect();
        }, 2000);
      }
    };

    void connect();

    return () => {
      cancelled = true;
      esRef.current?.close();
      clearTimeout(retryTimeout);
    };
  }, [url]);

  return data;
}
