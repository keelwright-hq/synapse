const router = require('express').Router();

router.get('/', listUsers);

router.get('/:id/orders', async (req, res) => {
  res.json([]);
});

function listUsers(req, res) {
  res.json([]);
}

module.exports = router;
