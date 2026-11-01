export function userFacingError(value: unknown, fallback: string): string {
  if (value instanceof DOMException && value.name === "AbortError") {
    return "A operação foi cancelada.";
  }

  const message = value instanceof Error ? value.message.trim() : typeof value === "string" ? value.trim() : "";
  if (!message) return fallback;
  const normalized = message.toLocaleLowerCase("en-US");

  if (/failed to fetch|fetch failed|networkerror|network request failed|load failed/.test(normalized)) {
    return "Não foi possível conectar à API do NÓDULO. Verifique sua conexão e se o servidor está ativo; depois, tente novamente.";
  }
  if (/context canceled|context cancelled|request was aborted|the operation was aborted/.test(normalized)) {
    return "A operação foi interrompida antes de terminar. Tente novamente; se era uma ingestão de arquivo, use a opção de continuar no monitor de processamento.";
  }
  if (/validate retry file|persisted chunk .* does not match|uploaded file chunk count|saved progress .* exceeds/.test(normalized)) {
    return "Não foi possível continuar porque o conteúdo extraído não corresponde aos fragmentos já salvos. Selecione uma cópia idêntica do arquivo original, sem alterações; se persistir, avise o administrador.";
  }
  if (/timeout|timed out|deadline exceeded/.test(normalized)) {
    return "O serviço demorou para responder. Verifique se a API e os serviços de IA estão disponíveis e tente novamente.";
  }
  if (/connection refused|dial tcp|no such host|name resolution/.test(normalized)) {
    return "Não foi possível alcançar um serviço necessário. Verifique se a API e o serviço de IA estão em execução e tente novamente.";
  }
  if (/invalid credentials|invalid email or password|incorrect password|wrong password/.test(normalized)) {
    return "E-mail ou senha incorretos. Confira os dados e tente novamente.";
  }
  if (/already exists|duplicate key|email.*(taken|registered)/.test(normalized)) {
    return "Já existe uma conta com esse e-mail. Entre na conta ou use outro endereço.";
  }
  if (/queue.*full|queue is full|too many requests/.test(normalized)) {
    return "O sistema está com muitas tarefas em andamento. Aguarde um pouco e tente novamente.";
  }
  if (/unsupported media type|unsupported file|unsupported format|file type.*not supported/.test(normalized)) {
    return "Este formato de arquivo não é aceito. Confira os formatos indicados na tela e selecione outro arquivo.";
  }
  if (/payload too large|file too large|request entity too large/.test(normalized)) {
    return "O arquivo enviado ultrapassa o tamanho permitido. Reduza o tamanho e tente novamente.";
  }
  if (/required field|field.*required|missing required|is required/.test(normalized)) {
    return "Faltam informações obrigatórias. Confira os campos indicados e tente novamente.";
  }
  if (/not found|no rows in result set/.test(normalized)) {
    return "O item solicitado não foi encontrado. Atualize a página e tente novamente.";
  }
  if (/unauthorized|not authenticated|invalid token|token expired/.test(normalized)) {
    return "Sua sessão expirou ou não é válida. Entre novamente para continuar.";
  }
  if (/forbidden|access denied|not authorized/.test(normalized)) {
    return "Você não tem permissão para realizar esta ação. Se acredita que isso está incorreto, fale com um administrador.";
  }
  if (/provider|inference|embedding|model.*not found/.test(normalized)) {
    return "O provedor de IA não conseguiu concluir a solicitação. Verifique se o serviço está disponível e se o modelo necessário está configurado; depois, tente novamente.";
  }
  if (/http\s*4\d\d/.test(normalized)) {
    return "A solicitação não pôde ser concluída. Confira os dados enviados e tente novamente.";
  }
  if (/http\s*5\d\d|internal server error|internal error/.test(normalized)) {
    return "O servidor encontrou um problema ao processar a solicitação. Tente novamente; se persistir, avise o administrador.";
  }

  // Preserve messages already written in Portuguese by the API.
  if (/[áàâãéêíóôõúüç]/i.test(message) || /\b(não|nao|foi|está|esta|erro|falha|usuário|utilizador|arquivo|ficheiro|conversa|cadastro|senha|e-mail|permissão|solicitação|serviço|documento|informe|envie|selecione|inválid[oa]s?)\b/i.test(message)) {
    return message;
  }

  // Avoid showing English implementation details or raw network/SQL errors.
  return fallback;
}

export async function apiErrorMessage(response: Response, fallback: string): Promise<string> {
  const body = await response.text().catch(() => "");
  if (body) {
    try {
      const parsed = JSON.parse(body) as { error?: unknown; message?: unknown };
      const value = parsed.error ?? parsed.message;
      if (typeof value === "string" && value.trim()) {
        return userFacingError(value, statusFallback(response.status, fallback));
      }
    } catch {
      // O servidor também pode responder texto simples.
    }
    const message = userFacingError(body, statusFallback(response.status, fallback));
    if (message !== statusFallback(response.status, fallback)) return message;
  }
  return statusFallback(response.status, fallback);
}

function statusFallback(status: number, fallback: string): string {
  if (status === 401) return "Sua sessão expirou ou os dados de acesso não são válidos. Entre novamente e tente de novo.";
  if (status === 403) return "Seu perfil não tem autorização para esta ação. Fale com um administrador se precisar de acesso.";
  if (status === 404) return "O item solicitado não foi encontrado. Atualize a página e tente novamente.";
  if (status === 413) return "O arquivo ou os dados enviados excedem o limite permitido. Reduza o tamanho e tente novamente.";
  if (status === 408 || status === 504) return "A solicitação demorou demais e foi interrompida. Tente novamente; se persistir, avise o administrador.";
  if (status === 429) return "O sistema recebeu muitas solicitações ao mesmo tempo. Aguarde um pouco e tente novamente.";
  if (status === 409) return "Esta ação não pode ser concluída porque os dados foram alterados ou já estão sendo processados. Atualize a tela e tente novamente.";
  if (status >= 500) return "O servidor encontrou um problema ao processar a solicitação. Tente novamente; se persistir, avise o administrador.";
  return fallback;
}
