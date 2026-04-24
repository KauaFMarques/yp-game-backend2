-- ============================================================
-- Schema: TCC Simulador Gamificado de Metodologia Ágil
-- ============================================================

DROP TABLE IF EXISTS response CASCADE;
DROP TABLE IF EXISTS choice CASCADE;
DROP TABLE IF EXISTS scenario CASCADE;
DROP TABLE IF EXISTS team CASCADE;
DROP TABLE IF EXISTS stage CASCADE;
DROP TABLE IF EXISTS room CASCADE;

-- Sala criada pelo professor
CREATE TABLE room (
    id        CHAR(7) PRIMARY KEY,
    name      VARCHAR(255) NOT NULL,
    professor VARCHAR(255) NOT NULL
);

-- Etapa da sala (semana 1, 2, 3…)
-- Uma sala pode ter várias etapas abertas em momentos diferentes
CREATE TABLE stage (
    id         SERIAL PRIMARY KEY,
    room       CHAR(7) REFERENCES room NOT NULL,
    number     INT     NOT NULL,  -- 1, 2, 3…
    label      VARCHAR(255),      -- ex: "Semana 1 — Primeiro contato"
    active     BOOLEAN DEFAULT TRUE NOT NULL,
    created_at TIMESTAMP DEFAULT NOW() NOT NULL,
    UNIQUE (room, number)
);

-- Time/grupo criado pelo aluno ao entrar na sala
CREATE TABLE team (
    id         SERIAL PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    room       CHAR(7) REFERENCES room NOT NULL,
    finished   BOOLEAN DEFAULT FALSE NOT NULL,
    created_at TIMESTAMP DEFAULT NOW() NOT NULL
);

-- Cenário criado pelo professor via API
CREATE TABLE scenario (
    id          SERIAL PRIMARY KEY,
    room        CHAR(7) REFERENCES room NOT NULL,
    stage       INT NOT NULL DEFAULT 1,      -- em qual etapa este cenário aparece
    title       VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    order_index INT  NOT NULL DEFAULT 0,     -- ordem de exibição dentro da etapa
    created_at  TIMESTAMP DEFAULT NOW() NOT NULL
);

-- Opções de escolha para cada cenário
-- O professor cadastra junto com o cenário
CREATE TABLE choice (
    id          SERIAL PRIMARY KEY,
    scenario_id INT REFERENCES scenario NOT NULL,
    text        TEXT NOT NULL,
    is_best     BOOLEAN DEFAULT FALSE NOT NULL,  -- se é a melhor decisão
    feedback    TEXT NOT NULL                    -- explicação mostrada imediatamente após responder
);

-- Resposta registrada pelo time
CREATE TABLE response (
    id            SERIAL PRIMARY KEY,
    team_id       INT REFERENCES team     NOT NULL,
    scenario_id   INT REFERENCES scenario NOT NULL,
    choice_id     INT REFERENCES choice   NOT NULL,
    is_best       BOOLEAN NOT NULL,
    response_time_ms INT,                        -- tempo de resposta em milissegundos
    responded_at  TIMESTAMP DEFAULT NOW() NOT NULL,
    UNIQUE (team_id, scenario_id)                -- um time só responde cada cenário uma vez
);