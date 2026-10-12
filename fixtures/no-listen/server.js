// Fixture (plan §11.5): the process starts but never listens on $PORT.
// Expect `failed` from the health check, with the previous live version
// (if any) still serving.
setInterval(() => {}, 1 << 30);
