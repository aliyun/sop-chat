/**
 * Layout Component
 * Wraps protected routes with navbar
 */
import Navbar from './Navbar';

function Layout({ children }) {
  return (
    <>
      <Navbar />
      <div className="app-content">
        {children}
      </div>
    </>
  );
}

export default Layout;
