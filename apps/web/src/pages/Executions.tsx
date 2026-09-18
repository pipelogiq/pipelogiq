import { Navigate, useLocation } from 'react-router-dom';

/** Preserve old bookmarks now that Pipelines is the execution history. */
export default function ExecutionsRedirect() {
  const { search, hash } = useLocation();
  return <Navigate to={{ pathname: '/pipelines', search, hash }} replace />;
}
