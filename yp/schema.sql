-- Sala criada pelo professor
CREATE TABLE IF NOT EXISTS room (
    id        CHAR(7) PRIMARY KEY,
    name      VARCHAR(255) NOT NULL,
    professor VARCHAR(255) NOT NULL
);

-- Time/grupo criado pelo aluno ao entrar na sala
CREATE TABLE IF NOT EXISTS team (
    id         SERIAL PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    room       CHAR(7) REFERENCES room NOT NULL,
    finished   BOOLEAN DEFAULT FALSE NOT NULL,
    created_at TIMESTAMP DEFAULT NOW() NOT NULL
);

-- Cenário (agora é basicamente a pergunta)
CREATE TABLE IF NOT EXISTS scenario (
    id          SERIAL PRIMARY KEY,
    room        CHAR(7) REFERENCES room NOT NULL,
    title       VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    order_index INT NOT NULL DEFAULT 0, -- ordem das perguntas
    created_at  TIMESTAMP DEFAULT NOW() NOT NULL
);

-- Opções de resposta
CREATE TABLE IF NOT EXISTS choice (
    id          SERIAL PRIMARY KEY,
    scenario_id INT REFERENCES scenario NOT NULL,
    text        TEXT NOT NULL,
    is_best     BOOLEAN DEFAULT FALSE NOT NULL,
    feedback    TEXT NOT NULL
);

-- Respostas do time
CREATE TABLE IF NOT EXISTS response (
    id            SERIAL PRIMARY KEY,
    team_id       INT REFERENCES team     NOT NULL,
    scenario_id   INT REFERENCES scenario NOT NULL,
    choice_id     INT REFERENCES choice   NOT NULL,
    is_best       BOOLEAN NOT NULL,
    response_time_ms INT,
    responded_at  TIMESTAMP DEFAULT NOW() NOT NULL,
    UNIQUE (team_id, scenario_id)
);