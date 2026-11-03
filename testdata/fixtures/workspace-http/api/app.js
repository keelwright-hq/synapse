const express = require('express');
const users = require('./routes/users');

const app = express();

// Mounts a router under the versioned API prefix.
function mountVersioned(suffix, ...handlers) {
  app.use(`/v1${suffix}`, ...handlers);
}

mountVersioned('/users', users);
app.get('/health', (req, res) => res.send('ok'));

module.exports = app;
