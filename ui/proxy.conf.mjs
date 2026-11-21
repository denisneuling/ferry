// The dev server (npm start) serves the interface and passes the API to a
// local ferry: `make run-nas` (or `make run`), on :8095.
const target = process.env.FERRY_URL ?? 'http://127.0.0.1:8095';

export default {
  '/api': { target, secure: false, changeOrigin: false },
};
